package testhelpers

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type PostgresContainer struct {
	testcontainers.Container
	PgConfig db.PgConfig
}

func CreatePostgresContainer(ctx context.Context) (*PostgresContainer, error) {
	testPW := "test"

	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			// todo: use the same image as PG deployment (currently we are running tests in GH, so the registry.redhat.io image will not spin up)
			// c9s is the upstream of rhel9
			Image: "quay.io/sclorg/postgresql-16-c9s:latest",
			Env: map[string]string{
				"POSTGRESQL_ADMIN_PASSWORD": testPW,
			},
			ExposedPorts: []string{"5432/tcp"},
			WaitingFor:   wait.ForLog("Starting server...").WithOccurrence(1).WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	}

	pgContainer, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("could not create generic postgres testcontainer: %w", err)
	}

	port, err := pgContainer.MappedPort(ctx, "5432")
	if err != nil {
		return nil, fmt.Errorf("failed to get testcontainer port: %w", err)
	}

	host, err := pgContainer.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get testcontainer host: %w", err)
	}

	return &PostgresContainer{
		Container: pgContainer,
		PgConfig: db.PgConfig{
			Host:     host,
			Port:     port.Port(),
			User:     "postgres",
			Password: testPW,
			Database: "postgres",
		},
	}, nil
}
