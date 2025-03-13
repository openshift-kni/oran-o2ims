/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package cluster

import (
	"embed"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

//go:embed db/migrations/*.sql
var migrations embed.FS

// StartMigration initiates the migration process for the cluster server database
func StartMigration() error {
	driver, err := iofs.New(migrations, "db/migrations")
	if err != nil {
		return fmt.Errorf("failed to create migrations source: %w", err)
	}

	password, exists := os.LookupEnv(utils.ClustersPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", utils.ClustersPasswordEnvName)
	}

	err = db.StartMigration(db.GetPgConfig(username, password, database), driver)
	if err != nil {
		return fmt.Errorf("failed to start migrations: %w", err)
	}

	return nil
}
