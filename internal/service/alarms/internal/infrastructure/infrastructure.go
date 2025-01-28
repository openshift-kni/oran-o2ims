package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

const resyncInterval = 1 * time.Hour

// Client is the interface that wraps the basic methods for the infrastructure clients
type Client interface {
	Name() string
	Setup() error

	FetchAll(context.Context) error

	GetObjectTypeID(objectID uuid.UUID) (uuid.UUID, error)
	GetAlarmDefinitionID(ObjectTypeID uuid.UUID, name, severity string) (uuid.UUID, error)

	ReSync(ctx context.Context)
}

// Infrastructure represents the infrastructure clients
type Infrastructure struct {
	Clients []Client
}

type AlarmDictionary = generated.AlarmDictionary

type AlarmDefinition map[AlarmDefinitionUniqueIdentifier]uuid.UUID
type AlarmDefinitionUniqueIdentifier struct {
	Name     string
	Severity string
}

// Init sets up the infrastructure clients and fetches all the data
func Init(ctx context.Context) (*Infrastructure, error) {
	// Currently only the cluster server is supported
	clients := []Client{&ClusterServer{}}

	for _, server := range clients {
		if err := server.Setup(); err != nil {
			return nil, fmt.Errorf("failed to setup %s: %w", server.Name(), err)
		}

		if err := server.FetchAll(ctx); err != nil {
			return nil, fmt.Errorf("failed to fetch all data for %s: %w", server.Name(), err)
		}

		server.ReSync(ctx)
	}

	return &Infrastructure{Clients: clients}, nil
}
