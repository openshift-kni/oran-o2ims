package dictionary

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/resourceserver"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

const (
	managedClusterVersionLabel = "openshiftVersion-major-minor"
	localClusterLabel          = "local-cluster"
)

// TODO: create a file with the resource type names once they are defined by the Resource Server
const (
	resourceTypeCluster = "Cluster"
	resourceTypeHub     = "Hub"
)

type AlarmDictionary struct {
	Client           crclient.Client
	AlarmsRepository *repo.AlarmsRepository

	RulesMap map[uuid.UUID][]monitoringv1.Rule
}

func New(client crclient.Client, ar *repo.AlarmsRepository) *AlarmDictionary {
	return &AlarmDictionary{
		Client:           client,
		AlarmsRepository: ar,

		RulesMap: make(map[uuid.UUID][]monitoringv1.Rule),
	}
}

// Load loads the alarm dictionary
func (r *AlarmDictionary) Load(ctx context.Context, resourceTypes *[]resourceserver.ResourceType) {
	slog.Info("loading alarm dictionaries")

	if resourceTypes == nil {
		slog.Warn("no resource types to load")
		return
	}

	type result struct {
		resourceTypeID uuid.UUID
		rules          []monitoringv1.Rule
		err            error
	}

	wg := sync.WaitGroup{}
	resultChannel := make(chan result)
	for _, resourceType := range *resourceTypes {
		wg.Add(1)
		go func(resourceType resourceserver.ResourceType) {
			var err error
			var rules []monitoringv1.Rule

			defer func() {
				wg.Done()
				resultChannel <- result{
					resourceTypeID: resourceType.ResourceTypeId,
					rules:          rules,
					err:            err,
				}
			}()

			// TODO: this needs to be updated once the resource type content is defined by the Resource Server. Not expected to be this simple.
			switch resourceType.Model {
			case resourceTypeCluster:
				rules, err = r.processCluster(ctx, resourceType.Version)
			case resourceTypeHub:
				rules, err = r.processHub(ctx)
			default:
				err = fmt.Errorf("unsupported resource type: %s", resourceType.Model)
			}
		}(resourceType)
	}

	go func() {
		wg.Wait()
		close(resultChannel)
	}()

	for res := range resultChannel {
		if res.err != nil {
			slog.Error("error fetching rules for resource type", "ResourceType ID", res.resourceTypeID, "error", res.err)
			continue
		}

		r.RulesMap[res.resourceTypeID] = res.rules
		slog.Info("loaded rules for resource type", "ResourceType ID", res.resourceTypeID, "rules count", len(res.rules))
	}

	r.syncDictionaries(ctx, *resourceTypes)
}

func (r *AlarmDictionary) processHub(ctx context.Context) ([]monitoringv1.Rule, error) {
	rules, err := r.getRules(ctx, r.Client)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetched rules for Hub cluster", "rules count", len(rules))
	return rules, nil
}

// Needed for testing
var getClientForCluster = k8s.NewClientForCluster

// processCluster processes a cluster resource type
func (r *AlarmDictionary) processCluster(ctx context.Context, version string) ([]monitoringv1.Rule, error) {
	cluster, err := r.getManagedCluster(ctx, version)
	if err != nil {
		return nil, err
	}

	cl, err := getClientForCluster(ctx, r.Client, cluster.Name)
	if err != nil {
		return nil, err
	}

	rules, err := r.getRules(ctx, cl)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetched rules for managed cluster", "cluster", cluster.Name, "version", version, "rules count", len(rules))
	return rules, nil
}

// getManagedCluster finds a single managed cluster with the given version
func (r *AlarmDictionary) getManagedCluster(ctx context.Context, version string) (*clusterv1.ManagedCluster, error) {
	// Match managed cluster with the given version and not local cluster
	selector := labels.NewSelector()
	versionSelector, _ := labels.NewRequirement(managedClusterVersionLabel, selection.Equals, []string{version})
	localClusterRequirement, _ := labels.NewRequirement(localClusterLabel, selection.NotEquals, []string{"true"})

	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	var managedClusters clusterv1.ManagedClusterList
	err := r.Client.List(ctxWithTimeout, &managedClusters, &crclient.ListOptions{
		LabelSelector: selector.Add(*versionSelector).Add(*localClusterRequirement),
		Limit:         1,
	})
	if err != nil {
		return nil, fmt.Errorf("error listing managed clusters: %w", err)
	}

	if len(managedClusters.Items) == 0 {
		return nil, fmt.Errorf("no managed cluster found with version %s", version)
	}

	return &managedClusters.Items[0], nil
}

// getRules gets rules defined within a PrometheusRule resource
func (r *AlarmDictionary) getRules(ctx context.Context, cl crclient.Client) ([]monitoringv1.Rule, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	var promRules monitoringv1.PrometheusRuleList
	err := cl.List(ctxWithTimeout, &promRules)
	if err != nil {
		return nil, fmt.Errorf("error listing prometheus rules: %w", err)
	}

	// Extract rules from PrometheusRule list
	var rules []monitoringv1.Rule
	for _, promRule := range promRules.Items {
		for _, group := range promRule.Spec.Groups {
			for _, rule := range group.Rules {
				// Only alerting rules are of interest (not recording rules)
				if rule.Alert != "" {
					rules = append(rules, rule)
				}
			}
		}
	}

	return rules, nil
}

// syncDictionaries synchronizes the alarm dictionaries in the database
func (r *AlarmDictionary) syncDictionaries(ctx context.Context, resourceTypes []resourceserver.ResourceType) {
	slog.Info("synchronizing alarm dictionaries in the database")

	// Delete Dictionaries that do not have a corresponding resource type
	ids := make([]any, 0, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		ids = append(ids, resourceType.ResourceTypeId)
	}

	err := r.AlarmsRepository.DeleteAlarmDictionariesNotIn(ctx, ids)
	if err != nil {
		slog.Error("error deleting dictionaries", "error", err)
	}

	// Upsert Dictionaries and Alarms
	// pgx.Pool is safe for concurrent use
	var wg sync.WaitGroup
	for _, rt := range resourceTypes {
		wg.Add(1)
		go func(resourceType resourceserver.ResourceType) {
			defer wg.Done()

			// Early to check in case it was not possible to collect rules for the resource type
			if _, ok := r.RulesMap[resourceType.ResourceTypeId]; !ok {
				slog.Error("no rules collected for resource type", "resourceTypeID", resourceType.ResourceTypeId)
				return
			}

			// Alarm Dictionary record
			alarmDict := models.AlarmDictionary{
				AlarmDictionaryVersion: resourceType.Version,
				EntityType:             fmt.Sprintf("%s-%s", resourceType.Model, resourceType.Version),
				Vendor:                 resourceType.Version,
				ResourceTypeID:         resourceType.ResourceTypeId,
			}

			// Upsert Alarm Dictionary
			alarmDictRecords, err := r.AlarmsRepository.UpsertAlarmDictionary(ctx, alarmDict)
			if err != nil {
				slog.Error("error upserting alarm dictionary", "resourceTypeID", resourceType.ResourceTypeId, "error", err)
				return
			}

			if len(alarmDictRecords) != 1 {
				// Should never happen
				slog.Error("unexpected number of Alarm Dictionary records, expected 1", "resourceTypeID", resourceType.ResourceTypeId, "count", len(alarmDictRecords))
				return
			}

			slog.Debug("alarm dictionary upserted", "resourceTypeID", resourceType.ResourceTypeId, "id", alarmDictRecords[0].AlarmDictionaryID)

			// Upsert will complain if there are rules with the same Alert and Severity
			// We need to filter them out. First occurrence wins.
			type uniqueAlarm struct {
				Alert    string
				Severity string
			}

			var filteredRules []monitoringv1.Rule
			exist := make(map[uniqueAlarm]bool)
			for _, rule := range r.RulesMap[resourceType.ResourceTypeId] {
				if !exist[uniqueAlarm{Alert: rule.Alert, Severity: rule.Labels["severity"]}] {
					exist[uniqueAlarm{Alert: rule.Alert, Severity: rule.Labels["severity"]}] = true
					filteredRules = append(filteredRules, rule)
				}
			}

			// Alarm Definition records
			records := make([]models.AlarmDefinition, 0, len(filteredRules))
			for _, rule := range filteredRules {
				// TODO: Add info from prometheus rules containing the rule such as the namespace
				additionalFields := map[string]string{"Expr": rule.Expr.String()}
				additionalFields["For"] = ""
				if rule.For != nil {
					additionalFields["For"] = string(*rule.For)
				}
				additionalFields["KeepFiringFor"] = ""
				if rule.KeepFiringFor != nil {
					additionalFields["KeepFiringFor"] = string(*rule.KeepFiringFor)
				}

				records = append(records, models.AlarmDefinition{
					AlarmName:             rule.Alert,
					AlarmLastChange:       alarmDict.AlarmDictionaryVersion,
					AlarmDescription:      fmt.Sprintf("Summary: %s\nDescription: %s", rule.Annotations["summary"], rule.Annotations["description"]),
					ProposedRepairActions: rule.Annotations["runbook_url"],
					AlarmAdditionalFields: additionalFields,
					AlarmDictionaryID:     alarmDictRecords[0].AlarmDictionaryID,
					Severity:              rule.Labels["severity"],
				})
			}

			// Upsert Alarm Definitions
			alarmDefinitionRecords, err := r.AlarmsRepository.UpsertAlarmDefinitions(ctx, records)
			if err != nil {
				slog.Error("error upserting alarm definitions", "resourceTypeID", resourceType.ResourceTypeId, "error", err)
				return
			}

			slog.Debug("alarm definitions upserted", "resourceTypeID", resourceType.ResourceTypeId, "count", len(alarmDefinitionRecords))

			// Delete Alarm Definitions that were not upserted
			alarmDefinitionIDs := make([]any, 0, len(alarmDefinitionRecords))
			for _, record := range alarmDefinitionRecords {
				alarmDefinitionIDs = append(alarmDefinitionIDs, record.AlarmDefinitionID)
			}
			err = r.AlarmsRepository.DeleteAlarmDefinitionsNotIn(ctx, alarmDefinitionIDs, resourceType.ResourceTypeId)
			if err != nil {
				slog.Error("error deleting alarm definitions", "resourceTypeID", resourceType.ResourceTypeId, "error", err)
			}
		}(rt)
	}

	wg.Wait()
	slog.Info("alarm dictionaries synchronized")
}
