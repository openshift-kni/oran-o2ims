package alarms

import (
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
)

//go:embed internal/db/migrations/*.sql
var migrations embed.FS

func StartAlarmsMigration() error {
	driver, err := iofs.New(migrations, "internal/db/migrations")
	if err != nil {
		return fmt.Errorf("failed to create migrations source: %w", err)
	}

	err = db.StartMigration(db.GetPgConfig(username, password, database), driver)
	if err != nil {
		return fmt.Errorf("failed to start migrations: %w", err)
	}

	return nil
}
