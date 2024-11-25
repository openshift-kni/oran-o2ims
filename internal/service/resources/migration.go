package resources

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

// StartResourcesMigration initiates the migration process for the resource server database
func StartResourcesMigration() error {
	driver, err := iofs.New(migrations, "db/migrations")
	if err != nil {
		return fmt.Errorf("failed to create migrations source: %w", err)
	}

	password, exists := os.LookupEnv(utils.ResourcesPasswordEnvName)
	if !exists {
		return fmt.Errorf("missing %s environment variable", utils.ResourcesPasswordEnvName)
	}

	err = db.StartMigration(db.GetPgConfig(username, password, database), driver)
	if err != nil {
		return fmt.Errorf("failed to start migrations: %w", err)
	}

	return nil
}
