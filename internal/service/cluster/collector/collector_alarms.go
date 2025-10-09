/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/intstr"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/cluster/db/models"
	common "github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

// AlarmsDataSource is the struct that holds the alarms data source
type AlarmsDataSource struct {
	dataSourceID uuid.UUID
	hubClient    client.WithWatch
	generationID int
}

// NewAlarmsDataSource creates a new AlarmsDataSource
func NewAlarmsDataSource() (DataSource, error) {
	// To avoid log from eventuallyFulfillRoot controller-runtime
	log.SetLogger(logr.Discard())

	// TODO: implement mechanism to refresh this client in case config changes
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client for hub: %w", err)
	}

	return &AlarmsDataSource{
		hubClient: hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *AlarmsDataSource) Name() string {
	return "Alarms"
}

// GetID returns the data source ID for this data source
func (d *AlarmsDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data
func (d *AlarmsDataSource) Init(uuid uuid.UUID, generationID int, _ chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID = generationID
}

// GetGenerationID retrieves the current generation id for this data source
func (d *AlarmsDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source
func (d *AlarmsDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// makeAlarmDictionaryIDToAlarmDefinitions fetches monitoring rules for each node cluster type and builds a map of alarm dictionary ID to alarm definitions
func (d *AlarmsDataSource) makeAlarmDictionaryIDToAlarmDefinitions(ctx context.Context, nodeClusterTypes []models.NodeClusterType) (map[uuid.UUID][]models.AlarmDefinition, error) {
	slog.Info("making alarm dictionary ID to alarm definitions map", "nodeClusterTypes count", len(nodeClusterTypes))

	// Fetch prometheus rules from managed clusters and hub
	nodeClusterTypeIDToMonitoringRules := d.makeNodeClusterTypeIDToMonitoringRules(ctx, nodeClusterTypes)

	// Continue if the fetch was successful for at least one node cluster type
	if len(nodeClusterTypeIDToMonitoringRules) == 0 {
		return nil, fmt.Errorf("failed to get monitoring rules")
	}

	return d.buildAlarmDictionaryIDToAlarmDefinitions(nodeClusterTypes, nodeClusterTypeIDToMonitoringRules), nil
}

// nodeClusterTypesWithAlarmDictionaryID filters out node cluster types that do not have an alarm dictionary ID
func nodeClusterTypesWithAlarmDictionaryID(nodeClusterTypes []models.NodeClusterType) []models.NodeClusterType {
	var filteredNodeClusterTypes []models.NodeClusterType
	for _, nodeClusterType := range nodeClusterTypes {
		if nodeClusterType.Extensions == nil {
			slog.Error("no extensions found for node cluster type", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID)
			continue
		}

		alarmDictionaryIDString := (*nodeClusterType.Extensions)[ctlrutils.ClusterAlarmDictionaryIDExtension]
		if alarmDictionaryIDString != nil {
			id, err := uuid.Parse(alarmDictionaryIDString.(string))
			if err != nil {
				slog.Error("error parsing alarm dictionary ID", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID, "error", err)
				continue
			}
			(*nodeClusterType.Extensions)[ctlrutils.ClusterAlarmDictionaryIDExtension] = id

			filteredNodeClusterTypes = append(filteredNodeClusterTypes, nodeClusterType)
		}
	}
	return filteredNodeClusterTypes
}

// makeNodeClusterTypeIDToMonitoringRules fetches monitoring rules for each node cluster type
func (d *AlarmsDataSource) makeNodeClusterTypeIDToMonitoringRules(ctx context.Context, nodeClusterTypes []models.NodeClusterType) map[uuid.UUID][]monitoringv1.Rule {
	slog.Info("making node cluster type ID to monitoring rules map", "nodeClusterTypes count", len(nodeClusterTypes))

	nodeClusterTypeIDToMonitoringRules := make(map[uuid.UUID][]monitoringv1.Rule)

	type result struct {
		nodeClusterTypeID uuid.UUID
		rules             []monitoringv1.Rule
		err               error
	}

	wg := sync.WaitGroup{}
	resultChannel := make(chan result)
	for _, nct := range nodeClusterTypes {
		wg.Add(1)
		go func(nodeClusterType models.NodeClusterType) {
			var err error
			var rules []monitoringv1.Rule

			defer func() {
				resultChannel <- result{
					nodeClusterTypeID: nodeClusterType.NodeClusterTypeID,
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
			case ctlrutils.ClusterModelManagedCluster:
				rules, err = d.processManagedCluster(ctx, extensions.version)
			case ctlrutils.ClusterModelHubCluster:
				rules, err = d.processHub(ctx)
			default:
				err = fmt.Errorf("unsupported node cluster type: %s", extensions.model)
			}
		}(nct)
	}

	go func() {
		wg.Wait()
		close(resultChannel)
	}()

	// Collect results
	for res := range resultChannel {
		if res.err != nil {
			slog.Error("error fetching rules for node cluster type", "NodeClusterType ID", res.nodeClusterTypeID, "error", res.err)
			continue
		}

		nodeClusterTypeIDToMonitoringRules[res.nodeClusterTypeID] = res.rules
		slog.Info("loaded rules for node cluster type", "NodeClusterType ID", res.nodeClusterTypeID, "rules count", len(res.rules))
	}

	return nodeClusterTypeIDToMonitoringRules
}

// processHub processes the hub cluster
func (d *AlarmsDataSource) processHub(ctx context.Context) ([]monitoringv1.Rule, error) {
	rules, err := d.getRules(ctx, d.hubClient)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetched rules for Hub cluster", "rules count", len(rules))
	return rules, nil
}

// Needed for testing
var getClientForCluster = k8s.NewClientForCluster

// processManagedCluster processes a managed cluster
func (d *AlarmsDataSource) processManagedCluster(ctx context.Context, version string) ([]monitoringv1.Rule, error) {
	cluster, err := d.getManagedCluster(ctx, version)
	if err != nil {
		return nil, err
	}

	cl, err := getClientForCluster(ctx, d.hubClient, cluster.Name)
	if err != nil {
		return nil, err
	}

	rules, err := d.getRules(ctx, cl)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetched rules for managed cluster", "cluster", cluster.Name, "version", version, "rules count", len(rules))
	return rules, nil
}

// getManagedCluster finds a single managed cluster with the given version
func (d *AlarmsDataSource) getManagedCluster(ctx context.Context, version string) (*clusterv1.ManagedCluster, error) {
	// Match managed cluster with the given version and not local cluster
	selector := labels.NewSelector()
	versionSelector, _ := labels.NewRequirement(ctlrutils.OpenshiftVersionLabelName, selection.Equals, []string{version})
	localClusterRequirement, _ := labels.NewRequirement(ctlrutils.LocalClusterLabelName, selection.NotEquals, []string{"true"})

	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.ListRequestTimeout)
	defer cancel()

	var managedClusters clusterv1.ManagedClusterList
	err := d.hubClient.List(ctxWithTimeout, &managedClusters, &crclient.ListOptions{
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
func (d *AlarmsDataSource) getRules(ctx context.Context, cl crclient.Client) ([]monitoringv1.Rule, error) {
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

// buildAlarmDictionaryIDToAlarmDefinitionsMap builds a map of alarm dictionary ID to alarm definitions
func (d *AlarmsDataSource) buildAlarmDictionaryIDToAlarmDefinitions(nodeClusterTypes []models.NodeClusterType, nodeClusterTypeIDToMonitoringRules map[uuid.UUID][]monitoringv1.Rule) map[uuid.UUID][]models.AlarmDefinition {
	slog.Info("building alarm dictionary ID to alarm definitions map", "nodeClusterTypes count", len(nodeClusterTypes))

	alarmDictionaryIDToAlarmDefinitions := make(map[uuid.UUID][]models.AlarmDefinition)
	for _, nodeClusterType := range nodeClusterTypes {
		if _, ok := nodeClusterTypeIDToMonitoringRules[nodeClusterType.NodeClusterTypeID]; !ok {
			slog.Warn("no monitoring rules found for node cluster type", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID)
			continue
		}

		// Remove once fields are added to the NodeClusterType model
		extensions, err := getVendorExtensions(nodeClusterType)
		if err != nil {
			// Should never happen
			slog.Error("error getting vendor extensions", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID, "error", err)
			continue
		}

		// Only process node cluster types with an alarm dictionary ID
		alarmDictionaryID, ok := (*nodeClusterType.Extensions)[ctlrutils.ClusterAlarmDictionaryIDExtension].(uuid.UUID)
		if !ok || alarmDictionaryID == uuid.Nil {
			slog.Error("no alarm dictionary ID found for node cluster type", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID)
			continue
		}

		slog.Debug("filtering rules for node cluster type", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID)
		// Remove conflicting rules before creating alarm definitions
		filteredRules := d.getFilteredRules(nodeClusterTypeIDToMonitoringRules[nodeClusterType.NodeClusterTypeID])

		alarmDictionaryIDToAlarmDefinitions[alarmDictionaryID] = d.createAlarmDefinitions(filteredRules, alarmDictionaryID, extensions.version, false)
	}

	return alarmDictionaryIDToAlarmDefinitions
}

// getFilteredRules check to see if rule can potentially be skipped
func (d *AlarmsDataSource) getFilteredRules(monitoringRules []monitoringv1.Rule) []monitoringv1.Rule {
	// Upsert will complain if there are rules with the same Alert and Severity
	// We need to filter them out. First occurrence wins.
	type uniqueAlarm struct {
		Alert    string
		Severity string
	}

	var filteredRules []monitoringv1.Rule
	exist := make(map[uniqueAlarm]bool)

	for _, rule := range monitoringRules {
		severity, ok := rule.Labels["severity"]
		if !ok {
			slog.Warn("rule missing severity label", "alert", rule.Alert)
		}

		key := uniqueAlarm{
			Alert:    rule.Alert,
			Severity: severity,
		}

		if !exist[key] {
			exist[key] = true
			filteredRules = append(filteredRules, rule)
		} else {
			slog.Warn("Duplicate rules found", "rule", rule)
		}
	}
	return filteredRules
}

// createAlarmDefinitions creates alarm definitions from prometheus rules
func (d *AlarmsDataSource) createAlarmDefinitions(rules []monitoringv1.Rule, alarmDictionaryID uuid.UUID, version string, isThanosRule bool) []models.AlarmDefinition {
	var records []models.AlarmDefinition

	for _, rule := range rules {
		additionalFields := map[string]any{"Expr": rule.Expr.String()}
		if rule.For != nil {
			additionalFields["For"] = string(*rule.For)
		}
		if rule.KeepFiringFor != nil {
			additionalFields["KeepFiringFor"] = string(*rule.KeepFiringFor)
		}

		// Add severity to additional fields
		additionalFields[ctlrutils.AlarmDefinitionSeverityField] = rule.Labels["severity"]

		//TODO: Add info from prometheus rules containing the rule such as the namespace

		summary := rule.Annotations["summary"]
		description := rule.Annotations["description"]
		runbookURL := rule.Annotations["runbook_url"]

		record := models.AlarmDefinition{
			AlarmName:             rule.Alert,
			AlarmLastChange:       version,
			AlarmChangeType:       string(common.ADDED),
			AlarmDescription:      fmt.Sprintf("Summary: %s\nDescription: %s", summary, description),
			ProposedRepairActions: runbookURL,
			ClearingType:          string(common.AUTOMATIC),
			AlarmAdditionalFields: &additionalFields,
			Severity:              rule.Labels[ctlrutils.AlarmDefinitionSeverityField],
			IsThanosRule:          isThanosRule,
		}

		// Set the alarm dictionary ID
		if alarmDictionaryID != uuid.Nil {
			record.AlarmDictionaryID = &alarmDictionaryID
		}

		records = append(records, record)
	}

	slog.Info("AlarmDefinitions from prometheus rules prepared", "count", len(records), "alarmDictionaryID", alarmDictionaryID)
	return records
}

// makeAlarmDictionaries creates alarm dictionaries from node cluster types
func (d *AlarmsDataSource) makeAlarmDictionaries(nodeClusterTypes []models.NodeClusterType) []models.AlarmDictionary {
	slog.Info("making alarm dictionaries", "nodeClusterTypes count", len(nodeClusterTypes))

	var alarmDictionaries []models.AlarmDictionary
	for _, nodeClusterType := range nodeClusterTypes {
		// Remove once fields are added to the NodeClusterType model
		extensions, err := getVendorExtensions(nodeClusterType)
		if err != nil {
			// Should never happen
			slog.Error("error getting vendor extensions", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID, "error", err)
			continue
		}

		alarmDictionaryID, ok := (*nodeClusterType.Extensions)[ctlrutils.ClusterAlarmDictionaryIDExtension].(uuid.UUID)
		if !ok || alarmDictionaryID == uuid.Nil {
			slog.Error("no alarm dictionary ID found for node cluster type", "NodeClusterType ID", nodeClusterType.NodeClusterTypeID)
			continue
		}

		alarmDictionary := models.AlarmDictionary{
			AlarmDictionaryID:      alarmDictionaryID,
			AlarmDictionaryVersion: extensions.version,
			EntityType:             fmt.Sprintf("%s-%s", extensions.model, extensions.version),
			Vendor:                 extensions.vendor,
			ManagementInterfaceID:  []string{string(common.AlarmDefinitionManagementInterfaceIdO2IMS)},
			PKNotificationField:    []string{"alarmDefinitionID"},
			NodeClusterTypeID:      nodeClusterType.NodeClusterTypeID,
			DataSourceID:           d.GetID(),
			GenerationID:           d.GetGenerationID(),
		}

		alarmDictionaries = append(alarmDictionaries, alarmDictionary)
	}

	return alarmDictionaries
}

const (
	alertRuleCustomConfigMapName  = "thanos-ruler-custom-rules"
	alertRuleDefaultConfigMapName = "thanos-ruler-default-rules"

	defaultConfigMapKey = "default_rules.yaml"
	customConfigMapKey  = "custom_rules.yaml"
)

type RuleSpec struct {
	Groups []RuleGroup `json:"groups,omitempty"`
}

type RuleGroup struct {
	Name                    string  `json:"name"`
	Interval                *string `json:"interval,omitempty"`
	Rules                   []Rule  `json:"rules,omitempty"`
	PartialResponseStrategy string  `json:"partial_response_strategy,omitempty"`
	Limit                   *int    `json:"limit,omitempty"`
}

type Rule struct {
	Record        string            `json:"record,omitempty"`
	Alert         string            `json:"alert,omitempty"`
	Expr          string            `json:"expr"`
	For           *string           `json:"for,omitempty"`
	KeepFiringFor *string           `json:"keep_firing_for,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// collectThanosRules collects Thanos rules from the config maps
func (d *AlarmsDataSource) collectThanosRules(ctx context.Context) ([]monitoringv1.Rule, error) {
	slog.Info("Collecting Thanos rules")
	var rules []monitoringv1.Rule

	ctxWithTimeout, cancel := context.WithTimeout(ctx, clients.SingleRequestTimeout)
	defer cancel()

	for configMapName, key := range map[string]string{alertRuleDefaultConfigMapName: defaultConfigMapKey, alertRuleCustomConfigMapName: customConfigMapKey} {
		// Get the config map
		configMap := &corev1.ConfigMap{}
		err := d.hubClient.Get(ctxWithTimeout, client.ObjectKey{
			Namespace: ctlrutils.OpenClusterManagementObservabilityNamespace,
			Name:      configMapName,
		}, configMap)
		if err != nil {
			if errors.IsNotFound(err) {
				slog.Warn("Config map not found", "configMap", configMapName)
				continue
			}

			return nil, fmt.Errorf("failed to get %s config map: %w", configMapName, err)
		}

		// Extract rules from the config map
		rulSpec := configMap.Data[key]
		if rulSpec != "" {
			// Unmarshal has issues handling intstr.IntOrString types
			// So we need to unmarshal it into a struct with string fields and then
			// convert it to the intstr.IntOrString type
			var spec RuleSpec
			err = yaml.Unmarshal([]byte(rulSpec), &spec)
			if err != nil {
				slog.Error("failed to unmarshal thanos prometheus rules spec", "configMap", configMapName, "error", err)
				continue
			}

			for _, group := range spec.Groups {
				for _, rule := range group.Rules {
					if rule.Alert != "" {
						rules = append(rules, monitoringv1.Rule{
							Alert:         rule.Alert,
							Expr:          intstr.IntOrString{Type: intstr.String, StrVal: rule.Expr},
							For:           (*monitoringv1.Duration)(rule.For),
							KeepFiringFor: (*monitoringv1.NonEmptyDuration)(rule.KeepFiringFor),
							Labels:        rule.Labels,
							Annotations:   rule.Annotations,
						})
					}
				}
			}
		} else {
			slog.Warn("No data found in thanos config map", "configMap", configMapName)
		}
	}

	slog.Debug("Collected thanos rules", "rules count", len(rules))
	return rules, nil
}

// makeThanosAlarmDefinitions creates alarm definitions from thanos rules
func (d *AlarmsDataSource) makeThanosAlarmDefinitions(rules []monitoringv1.Rule) ([]models.AlarmDefinition, error) {
	slog.Debug("Making Thanos alarm definitions", "rules count", len(rules))

	filteredRules := d.getFilteredRules(rules)

	// Get the local cluster to get the version
	version := ""
	var localCluster clusterv1.ManagedCluster
	err := d.hubClient.Get(context.TODO(), client.ObjectKey{
		Name: ctlrutils.LocalClusterLabelName,
	}, &localCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get local cluster: %w", err)
	}

	version = localCluster.Labels[ctlrutils.OpenshiftVersionLabelName]

	return d.createAlarmDefinitions(filteredRules, uuid.Nil, version, true), nil
}

// vendorExtensions holds the model, version, and vendor of a node cluster type
type vendorExtensions struct {
	model   string
	version string
	vendor  string
}

// getVendorExtensions gets the vendor extensions from the node cluster type. Should never return an error.
func getVendorExtensions(nodeClusterType models.NodeClusterType) (*vendorExtensions, error) {
	if nodeClusterType.Extensions == nil {
		return nil, fmt.Errorf("no extensions found for node cluster type %d", nodeClusterType.NodeClusterTypeID)
	}

	model, ok := (*nodeClusterType.Extensions)[ctlrutils.ClusterModelExtension].(string)
	if !ok {
		return nil, fmt.Errorf("no model extension found for node cluster type %s", nodeClusterType.NodeClusterTypeID)
	}

	version, ok := (*nodeClusterType.Extensions)[ctlrutils.ClusterVersionExtension].(string)
	if !ok {
		return nil, fmt.Errorf("no version extension found for node cluster type %s", nodeClusterType.NodeClusterTypeID)
	}

	vendor, ok := (*nodeClusterType.Extensions)[ctlrutils.ClusterVendorExtension].(string)
	if !ok {
		return nil, fmt.Errorf("no vendor extension found for node cluster type %s", nodeClusterType.NodeClusterTypeID)
	}

	return &vendorExtensions{
		model:   model,
		version: version,
		vendor:  vendor,
	}, nil
}
