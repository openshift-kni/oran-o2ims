package dictionary_collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/clusterserver"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
)

const pollInterval = 10 * time.Minute

// Collector is the struct that holds the alarms repository and the infrastructure clients
type Collector struct {
	AlarmsRepository *repo.AlarmsRepository
	Infrastructure   *infrastructure.Infrastructure

	hubClient crclient.Client
}

func New(ar *repo.AlarmsRepository, infra *infrastructure.Infrastructure) (*Collector, error) {
	ad := &Collector{
		AlarmsRepository: ar,
		Infrastructure:   infra,
	}

	// To avoid log from eventuallyFulfillRoot controller-runtime
	log.SetLogger(logr.Discard())

	// Create a client for the hub cluster
	hubClient, err := k8s.NewClientForHub()
	if err != nil {
		return nil, fmt.Errorf("failed to create client for hub cluster: %w", err)
	}

	ad.hubClient = hubClient

	return ad, nil
}

// Run starts the alarm dictionary collector
func (r *Collector) Run(ctx context.Context) {
	// Currently only the cluster server is supported
	for i := range r.Infrastructure.Clients {
		if r.Infrastructure.Clients[i].Name() == clusterserver.Name {
			r.executeNodeClusterTypeDictionaryService(ctx, r.Infrastructure.Clients[i].(*clusterserver.ClusterServer))
		}
	}
}

// executeNodeClusterTypeDictionaryService starts the NodeClusterTypeDictionaryService
func (r *Collector) executeNodeClusterTypeDictionaryService(ctx context.Context, clusterServerClient *clusterserver.ClusterServer) {
	c := &NodeClusterTypeDictionaryService{
		AlarmsRepository: r.AlarmsRepository,
		HubClient:        r.hubClient,
		RulesMap:         make(map[uuid.UUID][]monitoringv1.Rule),
	}

	// First execution
	c.loadNodeClusterTypeDictionaries(ctx, clusterServerClient.GetNodeClusterTypes())

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("context cancelled, stopping node cluster type dictionary service")
				return
			case <-time.After(pollInterval):
				c.loadNodeClusterTypeDictionaries(ctx, clusterServerClient.GetNodeClusterTypes())
			}
		}
	}()
}
