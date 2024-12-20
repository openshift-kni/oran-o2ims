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

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/clusterserver"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

const (
	managedClusterVersionLabel = "openshiftVersion"
	localClusterLabel          = "local-cluster"
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
func (r *AlarmDictionary) Load(ctx context.Context, nodeClusterTypes *[]clusterserver.NodeClusterType) {
	slog.Info("loading alarm dictionaries")

	if nodeClusterTypes == nil {
		slog.Warn("no node cluster types to load")
		return
	}

	type result struct {
		NodeClusterTypeID uuid.UUID
		rules             []monitoringv1.Rule
		err               error
	}

	wg := sync.WaitGroup{}
	resultChannel := make(chan result)
	for _, nct := range *nodeClusterTypes {
		wg.Add(1)
		go func(nodeClusterType clusterserver.NodeClusterType) {
			var err error
			var rules []monitoringv1.Rule

			defer func() {
				resultChannel <- result{
					NodeClusterTypeID: nodeClusterType.NodeClusterTypeId,
					rules:             rules,
					err:               err,
				}
				wg.Done()
			}()

			extensions, err := getVendorExtensions(nodeClusterType)
			if err != nil {
				// Should never happen
				err = fmt.Errorf("error getting vendor extensions: %w", err)
				return
			}

			switch extensions.model {
			case utils.ClusterModelManagedCluster:
				rules, err = r.processManagedCluster(ctx, extensions.version)
			case utils.ClusterModelHubCluster:
				rules, err = r.processHub(ctx)
			default:
				err = fmt.Errorf("unsupported node cluster type: %s", extensions.model)
			}
		}(nct)
	}

	go func() {
		wg.Wait()
		close(resultChannel)
	}()

	for res := range resultChannel {
		if res.err != nil {
			slog.Error("error fetching rules for node cluster type", "NodeClusterType ID", res.NodeClusterTypeID, "error", res.err)
			continue
		}

		r.RulesMap[res.NodeClusterTypeID] = res.rules
		slog.Info("loaded rules for node cluster type", "NodeClusterType ID", res.NodeClusterTypeID, "rules count", len(res.rules))
	}

	r.syncDictionaries(ctx, *nodeClusterTypes)
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

// processManagedCluster processes a managed cluster
func (r *AlarmDictionary) processManagedCluster(ctx context.Context, version string) ([]monitoringv1.Rule, error) {
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

// getRules gets rules defined within a PrometheusRule
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
func (r *AlarmDictionary) syncDictionaries(ctx context.Context, nodeClusterTypes []clusterserver.NodeClusterType) {
	slog.Info("synchronizing alarm dictionaries in the database")

	// Delete Dictionaries that do not have a corresponding Node Cluster Type
	ids := make([]any, 0, len(nodeClusterTypes))
	for _, nodeClusterType := range nodeClusterTypes {
		ids = append(ids, nodeClusterType.NodeClusterTypeId)
	}

	err := r.AlarmsRepository.DeleteAlarmDictionariesNotIn(ctx, ids)
	if err != nil {
		slog.Error("error deleting dictionaries", "error", err)
	}

	// Upsert Dictionaries and Alarms
	// pgx.Pool is safe for concurrent use
	var wg sync.WaitGroup
	for _, nct := range nodeClusterTypes {
		wg.Add(1)
		go func(nodeClusterType clusterserver.NodeClusterType) {
			defer wg.Done()

			// Early to check in case it was not possible to collect rules for the node cluster type
			if _, ok := r.RulesMap[nodeClusterType.NodeClusterTypeId]; !ok {
				slog.Error("no rules collected for node cluster type", "NodeClusterTypeId", nodeClusterType.NodeClusterTypeId)
				return
			}

			extensions, err := getVendorExtensions(nodeClusterType)
			if err != nil {
				slog.Error("error getting vendor extensions", "error", err)
				return
			}

			// Alarm Dictionary record
			alarmDict := models.AlarmDictionary{
				AlarmDictionaryVersion: extensions.version,
				EntityType:             fmt.Sprintf("%s-%s", extensions.model, extensions.version),
				Vendor:                 extensions.vendor,
				ObjectTypeID:           nodeClusterType.NodeClusterTypeId,
			}

			// Upsert Alarm Dictionary
			alarmDictRecords, err := r.AlarmsRepository.UpsertAlarmDictionary(ctx, alarmDict)
			if err != nil {
				slog.Error("error upserting alarm dictionary", "NodeClusterTypeId", nodeClusterType.NodeClusterTypeId, "error", err)
				return
			}

			if len(alarmDictRecords) != 1 {
				// Should never happen
				slog.Error("unexpected number of Alarm Dictionary records, expected 1", "NodeClusterTypeId", nodeClusterType.NodeClusterTypeId, "count", len(alarmDictRecords))
				return
			}

			slog.Debug("alarm dictionary upserted", "NodeClusterTypeId", nodeClusterType.NodeClusterTypeId, "id", alarmDictRecords[0].AlarmDictionaryID)

			// Upsert will complain if there are rules with the same Alert and Severity
			// We need to filter them out. First occurrence wins.
			type uniqueAlarm struct {
				Alert    string
				Severity string
			}

			var filteredRules []monitoringv1.Rule
			exist := make(map[uniqueAlarm]bool)
			for _, rule := range r.RulesMap[nodeClusterType.NodeClusterTypeId] {
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
				slog.Error("error upserting alarm definitions", "NodeClusterTypeId", nodeClusterType.NodeClusterTypeId, "error", err)
				return
			}

			slog.Debug("alarm definitions upserted", "NodeClusterTypeId", nodeClusterType.NodeClusterTypeId, "count", len(alarmDefinitionRecords))

			// Delete Alarm Definitions that were not upserted
			alarmDefinitionIDs := make([]any, 0, len(alarmDefinitionRecords))
			for _, record := range alarmDefinitionRecords {
				alarmDefinitionIDs = append(alarmDefinitionIDs, record.AlarmDefinitionID)
			}
			err = r.AlarmsRepository.DeleteAlarmDefinitionsNotIn(ctx, alarmDefinitionIDs, nodeClusterType.NodeClusterTypeId)
			if err != nil {
				slog.Error("error deleting alarm definitions", "NodeClusterTypeId", nodeClusterType.NodeClusterTypeId, "error", err)
			}
		}(nct)
	}

	wg.Wait()
	slog.Info("alarm dictionaries synchronized")
}

type vendorExtensions struct {
	model   string
	version string
	vendor  string
}

// getVendorExtensions gets the vendor extensions from the node cluster type.
// Should never return an error.
func getVendorExtensions(nodeClusterType clusterserver.NodeClusterType) (*vendorExtensions, error) {
	if nodeClusterType.Extensions == nil {
		return nil, fmt.Errorf("no extensions found for node cluster type %d", nodeClusterType.NodeClusterTypeId)
	}

	model, ok := (*nodeClusterType.Extensions)[utils.ClusterModelExtension].(string)
	if !ok {
		return nil, fmt.Errorf("no model extension found for node cluster type %s", nodeClusterType.NodeClusterTypeId)
	}

	version, ok := (*nodeClusterType.Extensions)[utils.ClusterVersionExtension].(string)
	if !ok {
		return nil, fmt.Errorf("no version extension found for node cluster type %s", nodeClusterType.NodeClusterTypeId)
	}

	vendor, ok := (*nodeClusterType.Extensions)[utils.ClusterVendorExtension].(string)
	if !ok {
		return nil, fmt.Errorf("no vendor extension found for node cluster type %s", nodeClusterType.NodeClusterTypeId)
	}

	return &vendorExtensions{
		model:   model,
		version: version,
		vendor:  vendor,
	}, nil
}
