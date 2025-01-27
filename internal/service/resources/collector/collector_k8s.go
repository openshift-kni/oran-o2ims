package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	v1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/async"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

const clusterReflectorName = "cluster-reflector"

// Extensions
const (
	artifactResourceIDExtension = "artifactResourceId"
	profileNameExtension        = "profileName"
	profileDataExtension        = "profileData"
	adminClientCertExtension    = "admin_client_cert"
	adminClientKeyExtension     = "admin_client_key"
	adminUsernameExtension      = "admin_user"
	clusterCACertExtension      = "cluster_ca_cert"
	clusterAPIEndpointExtension = "cluster_api_endpoint"
)

// Values
const (
	adminAuthName  = "admin"
	k8sProfileName = "k8s"
)

// K8SDataSource defines an instance of a data source collector that interacts with the ACM search-api
type K8SDataSource struct {
	dataSourceID      uuid.UUID
	cloudID           uuid.UUID
	globalCloudID     uuid.UUID
	hubClient         client.WithWatch
	generationID      int
	AsyncChangeEvents chan<- *async.AsyncChangeEvent
}

// NewK8SDataSource creates a new instance of an ACM data source collector whose purpose is to collect data from the
// ACM search API to be included in the resource, resource pool, resource type, and deployment manager tables.
func NewK8SDataSource(cloudID, globalCloudID uuid.UUID) (DataSource, error) {
	// Create a k8s client for the hub to be able to retrieve managed cluster info
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client for hub: %w", err)
	}

	return &K8SDataSource{
		generationID:  0,
		cloudID:       cloudID,
		globalCloudID: globalCloudID,
		hubClient:     hubClient,
	}, nil
}

// Name returns the name of this data source
func (d *K8SDataSource) Name() string {
	return "K8S"
}

// GetID returns the data source ID for this data source
func (d *K8SDataSource) GetID() uuid.UUID {
	return d.dataSourceID
}

// Init initializes the data source with its configuration data; including the ID, the GenerationID, and its extension
// values if provided.
func (d *K8SDataSource) Init(uuid uuid.UUID, generationID int, asyncEventChannel chan<- *async.AsyncChangeEvent) {
	d.dataSourceID = uuid
	d.generationID = generationID
	d.AsyncChangeEvents = asyncEventChannel
}

// SetGenerationID sets the current generation id for this data source.  This value is expected to
// be restored from persistent storage at initialization time.
func (d *K8SDataSource) SetGenerationID(value int) {
	d.generationID = value
}

// GetGenerationID retrieves the current generation id for this data source.
func (d *K8SDataSource) GetGenerationID() int {
	return d.generationID
}

// IncrGenerationID increments the current generation id for this data source.
func (d *K8SDataSource) IncrGenerationID() int {
	d.generationID++
	return d.generationID
}

// makeCapacityInfo creates a map of strings of capacity attributes for the cluster
func (d *K8SDataSource) makeCapacityInfo(cluster *v1.ManagedCluster) map[string]string {
	results := make(map[string]string)
	tags := []string{"cpu", "ephemeral-storage", "hugepages-1Gi", "hugepages-2Mi", "memory", "pods"}
	for _, tag := range tags {
		if value, found := cluster.Status.Allocatable[v1.ResourceName(tag)]; found {
			results[tag] = value.String()
		}
	}
	return results
}

// getClusterClientConfig retrieves the cluster client config for the specified cluster.
func (d *K8SDataSource) getClusterClientConfig(ctx context.Context, clusterName string) (*clientcmdapi.Config, error) {
	// Fetch the kubeconfig for this cluster
	kubeConfig, err := k8s.GetClusterKubeConfigFromSecret(ctx, d.hubClient, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal cluster config: %w", err)
	}

	return config, nil
}

// getKubeconfig builds the extensions required for the deployment manager
func (d *K8SDataSource) getKubeconfig(ctx context.Context, clusterName string, extensions map[string]interface{}) error {
	// Fetch the kubeconfig for this cluster and provide the info as extensions to the returned object. This
	// is anticipated as a temporary measure since eventually SMO will require accessing the API using an OAuth token
	// acquired from an OAuth server.
	config, err := d.getClusterClientConfig(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get cluster config: %w", err)
	}

	var caData, url string
	if cluster, found := config.Clusters[clusterName]; found {
		caData = string(cluster.CertificateAuthorityData)
		url = cluster.Server
	}

	var adminUser, adminCert, adminKey string
	if authInfo, found := config.AuthInfos[adminAuthName]; found {
		adminUser = adminAuthName
		adminCert = string(authInfo.ClientCertificateData)
		adminKey = string(authInfo.ClientKeyData)
	} else {
		return fmt.Errorf("admin auth info not found")
	}

	extensions[profileNameExtension] = k8sProfileName
	extensions[profileDataExtension] = map[string]interface{}{
		adminClientCertExtension:    adminCert,
		adminClientKeyExtension:     adminKey,
		adminUsernameExtension:      adminUser,
		clusterCACertExtension:      caData,
		clusterAPIEndpointExtension: url,
	}

	return nil
}

// convertManagedClusterToDeploymentManager converts a ManagedCluster to a DeploymentManager object
func (d *K8SDataSource) convertManagedClusterToDeploymentManager(ctx context.Context, cluster *v1.ManagedCluster) (models.DeploymentManager, error) {
	clusterID, found := cluster.Labels["clusterID"]
	if !found {
		return models.DeploymentManager{}, fmt.Errorf("clusterID label not found in cluster %s", cluster.Name)
	}
	deploymentManagerID, err := uuid.Parse(clusterID)
	if err != nil {
		return models.DeploymentManager{}, fmt.Errorf("failed to parse from clusterID '%s' from %s", clusterID, cluster.Name)
	}

	url := ""
	for _, clientConfig := range cluster.Spec.ManagedClusterClientConfigs {
		if clientConfig.URL != "" {
			url = clientConfig.URL
			break
		}
	}

	if url == "" {
		return models.DeploymentManager{}, fmt.Errorf("failed to find URL for cluster %s", cluster.Name)
	}

	extensions := map[string]interface{}{}

	if templateID, found := cluster.Labels[utils.ClusterTemplateArtifactsLabel]; found {
		extensions[artifactResourceIDExtension] = templateID
	}

	err = d.getKubeconfig(ctx, cluster.Name, extensions)
	if err != nil {
		// TODO: turn this back into an error once we fix getting the Kubeconfig for the local-cluster
		slog.Warn("failed to get deployment manager extensions", "cluster", cluster.Name, "error", err)
	}

	to := models.DeploymentManager{
		DeploymentManagerID: deploymentManagerID,
		Name:                cluster.Name,
		Description:         cluster.Name,
		OCloudID:            d.cloudID,
		URL:                 url,
		Locations:           []string{}, // TODO: populate with locations from all pools
		Capabilities:        nil,
		CapacityInfo:        d.makeCapacityInfo(cluster),
		Extensions:          extensions,
		DataSourceID:        d.dataSourceID,
		GenerationID:        d.generationID,
		ExternalID:          "",
	}

	return to, nil
}

// handleClusterWatchEvent handles an async event received from the managed cluster watcher
func (d *K8SDataSource) handleClusterWatchEvent(ctx context.Context, cluster *v1.ManagedCluster, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleWatchEvent received for managed cluster", "agent", cluster.Name, "type", eventType)

	if eventType != async.Deleted {
		condition := meta.FindStatusCondition(cluster.Status.Conditions, "ManagedClusterConditionAvailable")
		if condition == nil || condition.Status == metav1.ConditionFalse {
			// This cluster is not yet available, so filter it out.
			slog.Warn("Managed cluster is not available; skipping", "cluster", cluster.Name, "condition", condition)
			return uuid.Nil, nil
		}

		if _, found := cluster.Labels[utils.ClusterTemplateArtifactsLabel]; !found {
			// The provisioning request which is managing the installation of this cluster is not yet fulfilled
			slog.Warn("Cluster provisioning request is not yet fulfilled; skipping", "cluster", cluster.Name)
			return uuid.Nil, nil
		}
	}

	record, err := d.convertManagedClusterToDeploymentManager(ctx, cluster)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to convert managed cluster to node cluster: %w", err)
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return uuid.Nil, fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    eventType,
		Object:       record}:
		return record.DeploymentManagerID, nil
	}
}

// HandleAsyncEvent handles an add/update/delete to an object received by from the Reflector.
func (d *K8SDataSource) HandleAsyncEvent(ctx context.Context, obj interface{}, eventType async.AsyncEventType) (uuid.UUID, error) {
	slog.Debug("handleWatchEvent received for store adapter", "type", eventType, "object", fmt.Sprintf("%T", obj))
	switch value := obj.(type) {
	case *v1.ManagedCluster:
		return d.handleClusterWatchEvent(ctx, value, eventType)
	default:
		// We are only watching for specific event types so this should happen.
		slog.Warn("Unknown object type", "type", fmt.Sprintf("%T", obj))
		return uuid.Nil, fmt.Errorf("unknown type: %T", obj)
	}
}

// HandleSyncComplete handles the end of a sync operation by sending an event down to the Collector.
func (d *K8SDataSource) HandleSyncComplete(ctx context.Context, objectType runtime.Object, keys []uuid.UUID) error {
	var object db.Model
	switch objectType.(type) {
	case *v1.ManagedCluster:
		object = models.DeploymentManager{}
	default:
		// This should never happen since we watch for specific types
		slog.Warn("Unknown object type", "type", fmt.Sprintf("%T", objectType))
		return nil
	}

	select {
	case <-ctx.Done():
		slog.Info("context cancelled while writing to async event channel; aborting")
		return fmt.Errorf("context cancelled; aborting")
	case d.AsyncChangeEvents <- &async.AsyncChangeEvent{
		DataSourceID: d.dataSourceID,
		EventType:    async.SyncComplete,
		Object:       object,
		Keys:         keys}:
		return nil
	}
}

// Watch starts a watcher for each of the resources supported by this data source.
// The watch is dispatched to a go routine.  If the context is canceled, then the watchers are stopped.
func (d *K8SDataSource) Watch(ctx context.Context) error {

	// The Reflector package uses a channel to signal stop events rather than a context, so use this go routine to
	// bridge the two worlds.
	stopCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		slog.Info("context canceled; stopping reflectors")
		close(stopCh)
	}()

	// Create a Reflector to watch ManagedCluster objects
	lister := cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			var managedClusterList v1.ManagedClusterList
			err := d.hubClient.List(ctx, &managedClusterList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error listing managed clusters: %w", err)
			}
			return &managedClusterList, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			var managedClusterList v1.ManagedClusterList
			w, err := d.hubClient.Watch(ctx, &managedClusterList, &client.ListOptions{Raw: &options})
			if err != nil {
				return nil, fmt.Errorf("error watching managed clusters: %w", err)
			}
			return w, nil
		},
		DisableChunking: false,
	}

	store := async.NewReflectorStore(&v1.ManagedCluster{})
	reflector := cache.NewNamedReflector(clusterReflectorName, &lister, &v1.ManagedCluster{}, store, time.Duration(0))
	slog.Info("starting cluster reflector")
	go reflector.Run(stopCh)

	// Start monitoring the store to process incoming events
	slog.Info("starting to receive from cluster reflector store")
	go store.Receive(ctx, d)

	return nil
}
