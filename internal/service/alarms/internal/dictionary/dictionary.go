package dictionary

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	cr_client "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/k8s_client"
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
	Client cr_client.Client

	RulesMap map[int][]monitoringv1.Rule
}

func New(client cr_client.Client) *AlarmDictionary {
	return &AlarmDictionary{
		Client:   client,
		RulesMap: make(map[int][]monitoringv1.Rule),
	}
}

// Load loads the alarm dictionary
func (r *AlarmDictionary) Load(ctx context.Context) {
	// TODO: List Resource Type from Resource Server
	resourceTypeList := getResourceTypes()

	type result struct {
		resourceTypeID int
		rules          []monitoringv1.Rule
		err            error
	}

	wg := sync.WaitGroup{}
	resultChannel := make(chan result)
	for _, resource := range resourceTypeList {
		wg.Add(1)
		go func(resource resourceType) {
			var err error
			var rules []monitoringv1.Rule

			defer func() {
				wg.Done()
				resultChannel <- result{
					resourceTypeID: resource.id,
					rules:          rules,
					err:            err,
				}
			}()

			// TODO: this needs to be updated once the resource type content is defined by the Resource Server. Not expected to be this simple.
			switch resource.model {
			case resourceTypeCluster:
				rules, err = r.processCluster(ctx, resource.version)
			case resourceTypeHub:
				rules, err = r.processHub(ctx)
				// TODO: Logic to process Hub cluster rules will be added after the ones for the managed cluster are implemented
			default:
				err = fmt.Errorf("unsupported resource type: %s", resource.model)
			}
		}(resource)
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

	// TODO: Load future alarm dictionary struct
	// TODO: Load alarm dictionary into DB
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
var getClientForCluster = k8s_client.NewClientForCluster

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

	ctxWithTimeout, cancel := context.WithTimeout(ctx, k8s_client.ListRequestTimeout)
	defer cancel()

	var managedClusters clusterv1.ManagedClusterList
	err := r.Client.List(ctxWithTimeout, &managedClusters, &cr_client.ListOptions{
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
func (r *AlarmDictionary) getRules(ctx context.Context, cl cr_client.Client) ([]monitoringv1.Rule, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, k8s_client.ListRequestTimeout)
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
			rules = append(rules, group.Rules...)
		}
	}

	return rules, nil
}

// TODO: Delete once Resource Type is defined by Resource Server
type resourceType struct {
	id      int
	model   string
	version string
}

// TODO: Replace with actual resource type list from Resource Server
func getResourceTypes() []resourceType {
	return []resourceType{
		{
			id:      1,
			model:   resourceTypeCluster,
			version: "4.14",
		},
		{
			id:      2,
			model:   resourceTypeCluster,
			version: "4.15",
		},
		{
			id:      3,
			model:   resourceTypeCluster,
			version: "4.16",
		},
	}
}
