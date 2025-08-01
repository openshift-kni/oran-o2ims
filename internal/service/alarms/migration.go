/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package alarms

import (
	"embed"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

//go:embed internal/db/migrations/*.sql
var migrations embed.FS

func StartAlarmsMigration() error {
	driver, err := iofs.New(migrations, "internal/db/migrations")
	if err != nil {
		return fmt.Errorf("failed to create migrations source: %w", err)
	}

	password, exists := os.LookupEnv(ctlrutils.AlarmsPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", ctlrutils.AlarmsPasswordEnvName)
	}

	err = db.StartMigration(db.GetPgConfig(username, password, database), driver)
	if err != nil {
		return fmt.Errorf("failed to start migrations: %w", err)
	}

	return nil
}
