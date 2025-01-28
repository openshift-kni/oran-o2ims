package repo_test

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pashagolub/pgxmock/v4"

	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	alarmsrepo "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
)

var _ = Describe("AlarmsRepository", func() {
	var (
		mock pgxmock.PgxPoolIface
		repo *alarmsrepo.AlarmsRepository
		ctx  context.Context
	)

	BeforeEach(func() {
		var err error
		mock, err = pgxmock.NewPool()
		Expect(err).NotTo(HaveOccurred())

		repo = &alarmsrepo.AlarmsRepository{
			Db: mock,
		}
		ctx = context.Background()
	})

	AfterEach(func() {
		mock.Close()
	})

	Describe("CreateServiceConfiguration", func() {
		dataModel := models.ServiceConfiguration{}

		When("no configuration exists", func() {
			It("creates a new configuration", func() {
				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s", dataModel.TableName())).
					WillReturnRows(pgxmock.NewRows([]string{"id", "retention_period", "created_at", "updated_at"}))

				mock.ExpectQuery(fmt.Sprintf("INSERT INTO %s", dataModel.TableName())).
					WithArgs(30).
					WillReturnRows(
						pgxmock.NewRows([]string{"id", "retention_period", "created_at", "updated_at"}).
							AddRow(uuid.New(), 30, time.Now(), time.Now()),
					)

				config, err := repo.CreateServiceConfiguration(ctx, 30)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.RetentionPeriod).To(Equal(30))

				// Verify all expectations were met
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
		When("one configuration exists", func() {
			It("returns the existing configuration as CreateServiceConfiguration is only called during init)", func() {
				existingID := uuid.New()
				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s", dataModel.TableName())).
					WillReturnRows(
						pgxmock.NewRows([]string{"id", "retention_period", "created_at", "updated_at"}).
							AddRow(existingID, 45, time.Now(), time.Now()),
					)

				config, err := repo.CreateServiceConfiguration(ctx, 30)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.RetentionPeriod).To(Equal(45)) // should return existing value, not input value
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})

		When("multiple configurations exist", func() {
			It("keeps first configuration and deletes others", func() {
				now := time.Now()
				id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()

				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s", dataModel.TableName())).
					WillReturnRows(
						pgxmock.NewRows([]string{"id", "retention_period", "created_at", "updated_at"}).
							AddRow(id1, 45, now, now).
							AddRow(id2, 60, now, now).
							AddRow(id3, 90, now, now),
					)

				// Expect deletion of extra configurations (IDs 2 and 3)
				mock.ExpectExec(fmt.Sprintf("DELETE FROM %s WHERE", dataModel.TableName())).
					WithArgs(id2, id3).
					WillReturnResult(pgxmock.NewResult("DELETE", 2))

				config, err := repo.CreateServiceConfiguration(ctx, 30)
				Expect(err).NotTo(HaveOccurred())
				Expect(config.RetentionPeriod).To(Equal(45)) // should keep first config's value
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})

		When("delete operation fails", func() {
			It("returns an error", func() {
				now := time.Now()
				id1, id2 := uuid.New(), uuid.New()

				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s", dataModel.TableName())).
					WillReturnRows(
						pgxmock.NewRows([]string{"id", "retention_period", "created_at", "updated_at"}).
							AddRow(id1, 45, now, now).
							AddRow(id2, 60, now, now),
					)

				// Simulate delete operation failure
				mock.ExpectExec(fmt.Sprintf("DELETE FROM %s WHERE", dataModel.TableName())).
					WithArgs(id2).
					WillReturnError(fmt.Errorf("database error"))

				config, err := repo.CreateServiceConfiguration(ctx, 30)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to delete additional service configurations"))
				Expect(config).To(BeNil())
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})

		When("initial query fails", func() {
			It("returns an error", func() {
				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s", dataModel.TableName())).
					WillReturnError(fmt.Errorf("database error"))

				config, err := repo.CreateServiceConfiguration(ctx, 30)
				Expect(err).To(HaveOccurred())
				Expect(config).To(BeNil())
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})
})
