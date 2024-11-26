package dictionary_definition

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/clusterserver"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

type AlarmDictionaryDefinition struct {
	Client           crclient.Client
	AlarmsRepository *repo.AlarmsRepository
	RulesMap         map[uuid.UUID][]monitoringv1.Rule
}

func New(client crclient.Client, ar *repo.AlarmsRepository) *AlarmDictionaryDefinition {
	return &AlarmDictionaryDefinition{
		Client:           client,
		AlarmsRepository: ar,
		RulesMap:         make(map[uuid.UUID][]monitoringv1.Rule),
	}
}

// Load loads the alarm dictionary and definition
func (r *AlarmDictionaryDefinition) Load(ctx context.Context, nodeClusterTypes *[]clusterserver.NodeClusterType) error {
	slog.Info("Loading alarm dictionaries and definitions")
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
	err := r.syncDictionaries(ctx, *nodeClusterTypes)
	if err != nil {
		return fmt.Errorf("failed to sync dictionary and definitions: %w", err)
	}

	return nil
}

func (r *AlarmDictionaryDefinition) processHub(ctx context.Context) ([]monitoringv1.Rule, error) {
	rules, err := r.getRules(ctx, r.Client)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetched rules for Hub cluster", "rules count", len(rules))
	return rules, nil
}

// processManagedCluster processes a managed cluster
func (r *AlarmDictionaryDefinition) processManagedCluster(ctx context.Context, version string) ([]monitoringv1.Rule, error) {
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

// Needed for testing
var getClientForCluster = k8s.NewClientForCluster

// getManagedCluster finds a single managed cluster with the given version
func (r *AlarmDictionaryDefinition) getManagedCluster(ctx context.Context, version string) (*clusterv1.ManagedCluster, error) {
	// Match managed cluster with the given version and not local cluster
	selector := labels.NewSelector()
	versionSelector, _ := labels.NewRequirement(utils.OpenshiftVersionLabelName, selection.Equals, []string{version})
	localClusterRequirement, _ := labels.NewRequirement(utils.LocalClusterLabelName, selection.NotEquals, []string{"true"})

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
func (r *AlarmDictionaryDefinition) getRules(ctx context.Context, cl crclient.Client) ([]monitoringv1.Rule, error) {
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
func (r *AlarmDictionaryDefinition) syncDictionaries(ctx context.Context, nodeClusterTypes []clusterserver.NodeClusterType) error {
	slog.Info("Synchronizing alarm dictionaries in the database")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute) // let's try to finish init with a set time.
	defer cancel()

	// First clean up outdated dictionaries
	r.deleteOutdatedDictionaries(ctx, nodeClusterTypes)

	// Then process all NodeClusterTypes concurrently
	if err := r.processNodeClusterTypes(ctx, nodeClusterTypes); err != nil {
		return fmt.Errorf("failed to process some NodeClusterTypes: %w", err)
	}

	slog.Info("Alarm dictionaries and corresponding AlarmDefinitions synchronized")
	return nil
}

// deleteOutdatedDictionaries remove any dictionary that may not be available anymore
func (r *AlarmDictionaryDefinition) deleteOutdatedDictionaries(ctx context.Context, nodeClusterTypes []clusterserver.NodeClusterType) {
	// Delete Dictionaries that do not have a corresponding NodeClusterType
	ids := make([]any, 0, len(nodeClusterTypes))
	for _, nodeClusterType := range nodeClusterTypes {
		ids = append(ids, nodeClusterType.NodeClusterTypeId)
	}

	if err := r.AlarmsRepository.DeleteAlarmDictionariesNotIn(ctx, ids); err != nil {
		slog.Warn("failed to delete alarm dictionaries", "error", err)
	}

	slog.Info("Outdated alarm dictionaries deleted successfully")
}

// processNodeClusterTypes process each nodeClusterType in parallel and return early with error if anything fails
func (r *AlarmDictionaryDefinition) processNodeClusterTypes(ctx context.Context, nodeClusterTypes []clusterserver.NodeClusterType) error {
	// Validate all rules exist before starting any processing
	r.validateRulesExist(nodeClusterTypes)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(5) // Limit concurrent operations to avoid overwhelming the database

	for _, rt := range nodeClusterTypes {
		nodeClusterType := rt // Create new variable to avoid data race
		g.Go(func() error {
			return r.processNodeClusterType(ctx, nodeClusterType)
		})
	}
	slog.Info("Waiting for all nodeClusterTypes to processed")
	return g.Wait() //nolint:wrapcheck
}

// validateRulesExist check if we have rules to work with that will be converted to alarm defs and warn otherwise
func (r *AlarmDictionaryDefinition) validateRulesExist(nodeClusterTypes []clusterserver.NodeClusterType) {
	var missingRules []uuid.UUID
	for _, nct := range nodeClusterTypes {
		if _, ok := r.RulesMap[nct.NodeClusterTypeId]; !ok {
			missingRules = append(missingRules, nct.NodeClusterTypeId)
		}
	}

	if len(missingRules) > 0 {
		slog.Warn("Rules missing for nodeClustertype", "missingRules", missingRules)
	}
}

// processNodeClusterType process dict and def for nodeClusterType
func (r *AlarmDictionaryDefinition) processNodeClusterType(ctx context.Context, nodeClusterType clusterserver.NodeClusterType) error {
	// Process dictionary_definition
	dictID, err := r.upsertAlarmDictionary(ctx, nodeClusterType)
	if err != nil {
		return fmt.Errorf("failed to upsert alarm dictionary_definition for NodeClusterType %s: %w",
			nodeClusterType.NodeClusterTypeId, err)
	}

	// Process alarm definitions
	if err := r.processAlarmDefinitions(ctx, nodeClusterType, dictID); err != nil {
		return fmt.Errorf("failed to process alarm definitions for NodeClusterType %s: %w",
			nodeClusterType.NodeClusterTypeId, err)
	}

	slog.Info("Successfully processed nodeClusterType", "nodeClusterType", nodeClusterType.NodeClusterTypeId)
	return nil
}

// upsertAlarmDictionary process dict for nodeClusterType
func (r *AlarmDictionaryDefinition) upsertAlarmDictionary(ctx context.Context, nodeClusterType clusterserver.NodeClusterType) (models.AlarmDictionary, error) {
	extensions, err := getVendorExtensions(nodeClusterType)
	if err != nil {
		return models.AlarmDictionary{}, fmt.Errorf("failed to get extensions for nodeClusterType  %s: %w", nodeClusterType.NodeClusterTypeId, err)
	}
	// Alarm Dictionary record
	alarmDict := models.AlarmDictionary{
		AlarmDictionaryVersion: extensions.version,
		EntityType:             fmt.Sprintf("%s-%s", extensions.model, extensions.version),
		Vendor:                 extensions.version,
		ObjectTypeID:           nodeClusterType.NodeClusterTypeId,
	}

	// Upsert Alarm Dictionary
	alarmDictRecords, err := r.AlarmsRepository.UpsertAlarmDictionary(ctx, alarmDict)
	if err != nil {
		return models.AlarmDictionary{}, fmt.Errorf("failed to upsert alarm dictionary_definition: %w", err)
	}

	if len(alarmDictRecords) != 1 {
		return models.AlarmDictionary{}, fmt.Errorf("unexpected number of Alarm Dictionary records, expected 1, got %d", len(alarmDictRecords))
	}

	slog.Debug("Alarm dictionary upserted", "nodeClusterType", nodeClusterType.NodeClusterTypeId, "alarmDictionaryID", alarmDictRecords[0].AlarmDictionaryID)
	return alarmDictRecords[0], nil
}

// upsertAlarmDictionary process def for nodeClusterType
func (r *AlarmDictionaryDefinition) processAlarmDefinitions(ctx context.Context, nodeClusterType clusterserver.NodeClusterType, ad models.AlarmDictionary) error {
	// Get filtered rules
	filteredRules := r.getFilteredRules(nodeClusterType.NodeClusterTypeId)

	// Create alarm definitions
	records := r.createAlarmDefinitions(filteredRules, ad, nodeClusterType)

	// Upsert and cleanup
	if err := r.upsertAndCleanupDefinitions(ctx, records, nodeClusterType.NodeClusterTypeId); err != nil {
		return fmt.Errorf("failed to upsert and cleanup definitions: %w", err)
	}

	slog.Info("Successfully processed all AlarmDefinitions", "nodeClusterType", nodeClusterType.NodeClusterTypeId, "alarmDict", ad.AlarmDictionaryID)
	return nil
}

// getFilteredRules check to see if rule can potentially be skipped
func (r *AlarmDictionaryDefinition) getFilteredRules(nodeClusterTypeID uuid.UUID) []monitoringv1.Rule {
	// Upsert will complain if there are rules with the same Alert and Severity
	// We need to filter them out. First occurrence wins.
	type uniqueAlarm struct {
		Alert    string
		Severity string
	}

	var filteredRules []monitoringv1.Rule
	exist := make(map[uniqueAlarm]bool)

	for _, rule := range r.RulesMap[nodeClusterTypeID] {
		severity, ok := rule.Labels["severity"]
		if !ok {
			slog.Warn("rule missing severity label", "alert", rule.Alert, "nodeClusterTypeID", nodeClusterTypeID)
		}

		key := uniqueAlarm{
			Alert:    rule.Alert,
			Severity: severity,
		}

		if !exist[key] {
			exist[key] = true
			filteredRules = append(filteredRules, rule)
		} else {
			slog.Warn("Duplicate rules found", "nodeClusterTypeID", nodeClusterTypeID, "rule", rule)
		}
	}
	return filteredRules
}

// createAlarmDefinitions create new alarm def for each rules
func (r *AlarmDictionaryDefinition) createAlarmDefinitions(rules []monitoringv1.Rule, ad models.AlarmDictionary, nodeClusterType clusterserver.NodeClusterType) []models.AlarmDefinition {
	var records []models.AlarmDefinition

	for _, rule := range rules {
		additionalFields := map[string]string{"Expr": rule.Expr.String()}
		if rule.For != nil {
			additionalFields["For"] = string(*rule.For)
		}
		if rule.KeepFiringFor != nil {
			additionalFields["KeepFiringFor"] = string(*rule.KeepFiringFor)
		}

		ntc, _ := json.Marshal(nodeClusterType)
		additionalFields["NodeClusterTypeData"] = string(ntc)

		//TODO: Add info from prometheus rules containing the rule such as the namespace

		summary := rule.Annotations["summary"]
		description := rule.Annotations["description"]
		runbookURL := rule.Annotations["runbook_url"]

		records = append(records, models.AlarmDefinition{
			AlarmName:             rule.Alert,
			AlarmLastChange:       ad.AlarmDictionaryVersion,
			AlarmDescription:      fmt.Sprintf("Summary: %s\nDescription: %s", summary, description),
			ProposedRepairActions: runbookURL,
			AlarmAdditionalFields: additionalFields,
			AlarmDictionaryID:     ad.AlarmDictionaryID,
			Severity:              rule.Labels["severity"],
		})
	}

	slog.Info("AlarmDefinitions from promrules prepared", "count", len(records))
	return records
}

// upsertAndCleanupDefinitions insert or update and finally remove defs if possible
func (r *AlarmDictionaryDefinition) upsertAndCleanupDefinitions(ctx context.Context, records []models.AlarmDefinition, nodeClusterTypeID uuid.UUID) error {
	// Upsert Alarm Definitions
	alarmDefinitionRecords, err := r.AlarmsRepository.UpsertAlarmDefinitions(ctx, records)
	if err != nil {
		return fmt.Errorf("failed to upsert alarm definitions: %w", err)
	}

	slog.Info("Alarm definitions upserted", "nodeClusterTypeID", nodeClusterTypeID, "count", len(alarmDefinitionRecords))

	// Delete Alarm Definitions that were not upserted
	alarmDefinitionIDs := make([]any, 0, len(alarmDefinitionRecords))
	for _, record := range alarmDefinitionRecords {
		alarmDefinitionIDs = append(alarmDefinitionIDs, record.AlarmDefinitionID)
	}

	count, err := r.AlarmsRepository.DeleteAlarmDefinitionsNotIn(ctx, alarmDefinitionIDs, nodeClusterTypeID)
	if err != nil {
		return fmt.Errorf("failed to delete alarm definitions: %w", err)
	}
	if count > 0 {
		slog.Info("Alarm definitions synced", "nodeClusterTypeID", nodeClusterTypeID, "delete count", count)
	}

	return nil
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
