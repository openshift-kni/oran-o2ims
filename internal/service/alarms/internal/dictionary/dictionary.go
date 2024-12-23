package dictionary

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

const pollInterval = 30 * time.Minute

// AlarmDictionary represents the dictionary of alarms
type AlarmDictionary struct {
	AlarmsRepository *repo.AlarmsRepository
	Infrastructure   *infrastructure.Infrastructure

	hubClient crclient.Client
}

// New creates a new AlarmDictionary instance
func New(ar *repo.AlarmsRepository, infra *infrastructure.Infrastructure) (*AlarmDictionary, error) {
	ad := &AlarmDictionary{
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

// Run starts the dictionary collector
func (r *AlarmDictionary) Run(ctx context.Context) {
	// Currently only the cluster server is supported
	for i := range r.Infrastructure.Clients {
		if r.Infrastructure.Clients[i].Name() == clusterserver.Name {
			r.executeClusterDictionaryCollector(ctx, r.Infrastructure.Clients[i])
		}
	}
}

// executeClusterDictionaryCollector executes the cluster dictionary collector
func (r *AlarmDictionary) executeClusterDictionaryCollector(ctx context.Context, infraClient infrastructure.Client) {
	c := &ClusterCollector{
		AlarmsRepository: r.AlarmsRepository,
		HubClient:        r.hubClient,
		RulesMap:         make(map[uuid.UUID][]monitoringv1.Rule),
	}

	// First execution
	c.loadClusterDictionaries(ctx, infraClient)

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("context cancelled, stopping cluster dictionary collector")
				return
			case <-time.After(pollInterval):
				c.loadClusterDictionaries(ctx, infraClient)
			}
		}
	}()
}
