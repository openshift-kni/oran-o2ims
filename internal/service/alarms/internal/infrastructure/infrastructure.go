/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/api/generated"
)

const resyncInterval = 1 * time.Hour

//go:generate mockgen -source=infrastructure.go -destination=generated/mock_infrastructure_client.generated.go -package=generated

// Client is the interface that wraps the basic methods for the infrastructure clients
type Client interface {
	Name() string
	Setup() error

	FetchAll(context.Context) error

	GetObjectTypeID(ctx context.Context, objectID uuid.UUID) (uuid.UUID, error)
	GetAlarmDefinitionID(ctx context.Context, ObjectTypeID uuid.UUID, name, severity string) (uuid.UUID, error)

	// Sync starts a background process to populate and keep up-to-date a local cache with data from the infrastructure servers
	Sync(ctx context.Context)
}

// Infrastructure represents the infrastructure clients
type Infrastructure struct {
	ClusterServer  Client
	ResourceServer Client
}

type AlarmDictionary = generated.AlarmDictionary

type AlarmDefinition map[AlarmDefinitionUniqueIdentifier]uuid.UUID
type AlarmDefinitionUniqueIdentifier struct {
	Name     string
	Severity string
}

// Init sets up the infrastructure clients and fetches all the data
func Init(ctx context.Context) (*Infrastructure, error) {
	// Initialize both cluster server (for CaaS alerts) and resource server (for hardware alerts)
	clusterServer := &ClusterServer{}
	resourceServer := &ResourceServer{}

	for _, server := range []Client{clusterServer, resourceServer} {
		if err := server.Setup(); err != nil {
			return nil, fmt.Errorf("failed to setup %s: %w", server.Name(), err)
		}

		server.Sync(ctx)
	}

	return &Infrastructure{
		ClusterServer:  clusterServer,
		ResourceServer: resourceServer,
	}, nil
}
