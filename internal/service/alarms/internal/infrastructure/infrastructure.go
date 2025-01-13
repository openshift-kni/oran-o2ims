package infrastructure

import (
	"context"
	"fmt"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/infrastructure/clusterserver"
)

// Client is the interface that wraps the basic methods for the infrastructure clients
type Client interface {
	Name() string
	Setup() error
	FetchAll(context.Context) error
}

// Infrastructure represents the infrastructure clients
type Infrastructure struct {
	Clients []Client
}

// Init sets up the infrastructure clients and fetches all the data
func Init(ctx context.Context) (*Infrastructure, error) {
	// Currently only the cluster server is supported
	clients := []Client{&clusterserver.ClusterServer{}}

	for _, server := range clients {
		if err := server.Setup(); err != nil {
			return nil, fmt.Errorf("failed to setup %s: %w", server.Name(), err)
		}

		if err := server.FetchAll(ctx); err != nil {
			return nil, fmt.Errorf("failed to fetch objects from %s: %w", server.Name(), err)
		}
	}

	return &Infrastructure{Clients: clients}, nil
}
