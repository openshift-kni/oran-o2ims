/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package repo_test

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pashagolub/pgxmock/v5"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	alarmsrepo "github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/repo"
	svcutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
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
		Expect(err).ToNot(HaveOccurred())

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
				Expect(err).ToNot(HaveOccurred())
				Expect(config.RetentionPeriod).To(Equal(30))

				// Verify all expectations were met
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
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
				Expect(err).ToNot(HaveOccurred())
				Expect(config.RetentionPeriod).To(Equal(45)) // should return existing value, not input value
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
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
				Expect(err).ToNot(HaveOccurred())
				Expect(config.RetentionPeriod).To(Equal(45)) // should keep first config's value
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
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
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("initial query fails", func() {
			It("returns an error", func() {
				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s", dataModel.TableName())).
					WillReturnError(fmt.Errorf("database error"))

				config, err := repo.CreateServiceConfiguration(ctx, 30)
				Expect(err).To(HaveOccurred())
				Expect(config).To(BeNil())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("GetAlarmEventRecords", func() {
		When("records exist", func() {
			It("returns all alarm event records", func() {
				now := time.Now()
				id1, id2 := uuid.New(), uuid.New()

				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s", models.AlarmEventRecord{}.TableName())).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"alarm_event_record_id", "alarm_raised_time", "alarm_acknowledged", "perceived_severity",
						}).
							AddRow(id1, now, false, api.CLEARED).
							AddRow(id2, now, true, api.INDETERMINATE),
					)

				records, err := repo.GetAlarmEventRecords(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(records).To(HaveLen(2))
				Expect(records[0].AlarmEventRecordID).To(Equal(id1))
				Expect(records[1].AlarmEventRecordID).To(Equal(id2))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("PatchAlarmEventRecordACK", func() {
		When("the alarm exists", func() {
			It("updates acknowledgment status of an alarm", func() {
				id := uuid.New()
				now := time.Now()
				record := &models.AlarmEventRecord{
					AlarmEventRecordID:    id,
					AlarmAcknowledged:     true,
					AlarmAcknowledgedTime: &now,
					PerceivedSeverity:     api.WARNING,
				}

				mock.ExpectQuery(fmt.Sprintf("UPDATE %s SET", models.AlarmEventRecord{}.TableName())).
					WithArgs(true, &now, api.WARNING, id).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"alarm_event_record_id", "alarm_acknowledged", "alarm_acknowledged_time", "alarm_cleared_time", "perceived_severity",
						}).AddRow(id, true, &now, &now, api.WARNING),
					)

				result, err := repo.PatchAlarmEventRecordACK(ctx, id, record)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.AlarmAcknowledged).To(BeTrue())
				Expect(result.AlarmAcknowledgedTime).To(Equal(&now))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("GetAlarmEventRecord", func() {
		When("record exist given ID", func() {
			It("returns one event record", func() {
				now := time.Now()
				id1 := uuid.New()

				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE \\(\"alarm_event_record_id\" = \\$\\d+\\)", models.AlarmEventRecord{}.TableName())).WithArgs(id1).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"alarm_event_record_id", "alarm_raised_time", "alarm_acknowledged", "perceived_severity",
						}).
							AddRow(id1, now, false, api.CLEARED),
					)

				records, err := repo.GetAlarmEventRecord(ctx, id1)
				Expect(err).ToNot(HaveOccurred())
				Expect(records.AlarmEventRecordID).To(Equal(id1))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("GetAlarmSubscription", func() {
		When("subscription exists", func() {
			It("retrieves a specific alarm subscription", func() {
				id := uuid.New()
				conId := uuid.NewString()
				callback := "http://example.com/callback"

				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE", models.AlarmSubscription{}.TableName())).
					WithArgs(id).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"subscription_id", "consumer_subscription_id", "callback",
						}).AddRow(id, &conId, callback),
					)

				subscription, err := repo.GetAlarmSubscription(ctx, id)
				Expect(err).ToNot(HaveOccurred())
				Expect(subscription.SubscriptionID).To(Equal(id))
				Expect(subscription.ConsumerSubscriptionID).To(Equal(&conId))
				Expect(subscription.Callback).To(Equal(callback))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("subscription doesn't exist", func() {
			It("returns error", func() {
				id := uuid.New()
				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE", models.AlarmSubscription{}.TableName())).
					WithArgs(id).
					WillReturnRows(pgxmock.NewRows([]string{}))

				subscription, err := repo.GetAlarmSubscription(ctx, id)
				Expect(err).To(HaveOccurred())
				Expect(subscription).To(BeNil())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("UpsertAlarmEventCaaSRecord", func() {
		When("upserting a single record", func() {
			It("successfully upserts alarm event records", func() {
				id := uuid.New()
				objectClass := "oran.o2ims.cluster.NodeCluster"
				records := []models.AlarmEventRecord{
					{
						AlarmRaisedTime:   time.Now(),
						PerceivedSeverity: api.WARNING,
						ObjectID:          &id,
						ObjectClass:       &objectClass,
						Fingerprint:       "9a9e2d82a78cf2b9",
						AlarmSource:       models.AlarmSourceCaaS,
					},
				}
				// Expect transaction begin
				mock.ExpectBegin()

				// object_class is included in the insert columns/args (R2 persistence)
				// and in the ON CONFLICT DO UPDATE SET excluded list.
				// Spot-check that object_class is present both in the INSERT column list and in the
				// ON CONFLICT DO UPDATE SET clause (R2 persistence), so a dropped object_class column
				// would fail the match rather than slip through a table-name-only prefix.
				mock.ExpectExec(`(?s)INSERT INTO alarm_event_record.*object_class.*ON CONFLICT.*object_class`).
					WithArgs(
						records[0].AlarmRaisedTime, records[0].AlarmClearedTime,
						records[0].AlarmAcknowledgedTime, records[0].AlarmAcknowledged,
						records[0].PerceivedSeverity, records[0].Extensions,
						records[0].ObjectID, records[0].ObjectTypeID, records[0].ObjectClass,
						records[0].AlarmStatus, records[0].Fingerprint,
						records[0].AlarmDefinitionID, records[0].ProbableCauseID, int64(0), records[0].AlarmSource,
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				// Expect transaction commit
				mock.ExpectCommit()

				// Expect the deferred rollback after commit
				mock.ExpectRollback()

				err := repo.WithTransaction(ctx, func(tx pgx.Tx) error {
					return repo.UpsertAlarmEventCaaSRecord(ctx, tx, records, int64(0))
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("upserting multiple records", func() {
			It("handles multiple records in a single upsert", func() {
				id1, id2 := uuid.New(), uuid.New()
				now := time.Now()
				records := []models.AlarmEventRecord{
					{
						AlarmRaisedTime:   now,
						PerceivedSeverity: api.WARNING,
						ObjectID:          &id1,
						Fingerprint:       "9a9e2d82a78cf2b7",
						AlarmSource:       models.AlarmSourceCaaS,
					},
					{
						AlarmRaisedTime:   now,
						PerceivedSeverity: api.CRITICAL,
						ObjectID:          &id2,
						Fingerprint:       "9a9e2d82a78cf2b9",
						AlarmSource:       models.AlarmSourceCaaS,
					},
				}

				// Expect transaction begin
				mock.ExpectBegin()

				// Spot-check that object_class is present both in the INSERT column list and in the
				// ON CONFLICT DO UPDATE SET clause (R2 persistence), so a dropped object_class column
				// would fail the match rather than slip through a table-name-only prefix.
				mock.ExpectExec(`(?s)INSERT INTO alarm_event_record.*object_class.*ON CONFLICT.*object_class`).
					WithArgs(
						records[0].AlarmRaisedTime, records[0].AlarmClearedTime,
						records[0].AlarmAcknowledgedTime, records[0].AlarmAcknowledged,
						records[0].PerceivedSeverity, records[0].Extensions,
						records[0].ObjectID, records[0].ObjectTypeID, records[0].ObjectClass,
						records[0].AlarmStatus, records[0].Fingerprint,
						records[0].AlarmDefinitionID, records[0].ProbableCauseID, int64(0), records[0].AlarmSource,
						records[1].AlarmRaisedTime, records[1].AlarmClearedTime,
						records[1].AlarmAcknowledgedTime, records[1].AlarmAcknowledged,
						records[1].PerceivedSeverity, records[1].Extensions,
						records[1].ObjectID, records[1].ObjectTypeID, records[1].ObjectClass,
						records[1].AlarmStatus, records[1].Fingerprint,
						records[1].AlarmDefinitionID, records[1].ProbableCauseID, int64(0), records[1].AlarmSource,
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 2))

				// Expect transaction commit
				mock.ExpectCommit()

				// Expect the deferred rollback after commit
				mock.ExpectRollback()

				err := repo.WithTransaction(ctx, func(tx pgx.Tx) error {
					return repo.UpsertAlarmEventCaaSRecord(ctx, tx, records, int64(0))
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("given an empty record list", func() {
			It("handles empty record list", func() {
				// Expect transaction begin
				mock.ExpectBegin()

				// No database operations should be performed with empty list
				// but we need to expect commit and rollback for transaction completion

				// Expect transaction commit
				mock.ExpectCommit()

				// Expect the deferred rollback after commit
				mock.ExpectRollback()
				err := repo.WithTransaction(ctx, func(tx pgx.Tx) error {
					return repo.UpsertAlarmEventCaaSRecord(ctx, tx, []models.AlarmEventRecord{}, int64(0))
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())

			})
		})
	})

	Describe("UpdateSubscriptionEventCursor", func() {
		When("update is successful", func() {
			It("updates subscription event cursor successfully", func() {
				subscription := models.AlarmSubscription{
					SubscriptionID: uuid.New(),
					EventCursor:    10,
				}

				mock.ExpectQuery(fmt.Sprintf("UPDATE %s SET", subscription.TableName())).
					WithArgs(subscription.EventCursor, subscription.SubscriptionID).
					WillReturnRows(pgxmock.NewRows([]string{"subscription_id"}).AddRow(subscription.SubscriptionID))

				err := repo.UpdateSubscriptionEventCursor(ctx, subscription)
				Expect(err).ToNot(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("update fails", func() {
			It("returns error", func() {
				subscription := models.AlarmSubscription{
					SubscriptionID: uuid.New(),
					EventCursor:    10,
				}

				mock.ExpectQuery(fmt.Sprintf("UPDATE %s SET", subscription.TableName())).
					WithArgs(subscription.EventCursor, subscription.SubscriptionID).
					WillReturnError(fmt.Errorf("database error"))

				err := repo.UpdateSubscriptionEventCursor(ctx, subscription)
				Expect(err).To(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("ResolveStaleAlarmEventCaaSRecord", func() {
		When("resolving notifications", func() {
			It("resolves notifications that are older than current generation ID", func() {
				clearTime := time.Now()
				alarmsrepo.TimeNow = func() time.Time {
					return clearTime
				}

				// Expect transaction begin
				mock.ExpectBegin()

				mock.ExpectQuery(fmt.Sprintf("UPDATE %s SET", models.AlarmEventRecord{}.TableName())).
					WithArgs(api.Resolved, api.CLEARED, clearTime, int64(2), models.AlarmSourceCaaS, models.AlarmSourceHardware, api.Resolved).
					WillReturnRows(pgxmock.NewRows([]string{"alarm_event_record_id"}).AddRow(uuid.New()))

				// Expect transaction commit
				mock.ExpectCommit()

				// Expect the deferred rollback after commit
				mock.ExpectRollback()

				err := repo.WithTransaction(ctx, func(tx pgx.Tx) error {
					return repo.ResolveStaleAlarmEventCaaSRecord(ctx, tx, int64(2))
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("GetServiceConfigurations", func() {
		When("records exist", func() {
			It("returns all service configurations", func() {
				id1 := uuid.New()
				s := models.ServiceConfiguration{
					RetentionPeriod: 5,
				}
				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE \\(1=1\\)", models.ServiceConfiguration{}.TableName())).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"id", "retention_period", "extensions",
						}).
							AddRow(id1, s.RetentionPeriod, s.Extensions),
					)

				records, err := repo.GetServiceConfigurations(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(records).To(HaveLen(1))
				Expect(records[0].ID).To(Equal(id1))
				Expect(records[0].RetentionPeriod).To(Equal(s.RetentionPeriod))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("UpdateServiceConfiguration", func() {
		When("records exist", func() {
			It("returns all service configurations", func() {
				s := models.ServiceConfiguration{
					ID:              uuid.New(),
					RetentionPeriod: 5,
				}
				mock.ExpectQuery(fmt.Sprintf("UPDATE %s SET", models.ServiceConfiguration{}.TableName())).
					WithArgs(s.Extensions, s.RetentionPeriod, s.ID).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"extensions", "retention_period", "id",
						}).AddRow(s.Extensions, s.RetentionPeriod, s.ID),
					)

				record, err := repo.UpdateServiceConfiguration(ctx, s.ID, &s)
				Expect(err).ToNot(HaveOccurred())
				Expect(record.ID).To(Equal(s.ID))
				Expect(record.RetentionPeriod).To(Equal(s.RetentionPeriod))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("GetAlarmSubscriptions", func() {
		When("records exist", func() {
			It("returns all alarms subscription", func() {
				id1, id2 := uuid.New(), uuid.New()
				conID1, conID2 := uuid.NewString(), uuid.NewString()
				f := api.AlarmSubscriptionInfoFilterNEW
				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE \\(1=1\\)", models.AlarmSubscription{}.TableName())).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"subscription_id", "consumer_subscription_id", "filter", "callback",
						}).
							AddRow(id1, &conID1, &f, "http://test.com/callback").
							AddRow(id2, &conID2, &f, "http://test.com/callback"),
					)

				records, err := repo.GetAlarmSubscriptions(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(records).To(HaveLen(2))
				Expect(records[0].SubscriptionID).To(Equal(id1))
				Expect(records[1].SubscriptionID).To(Equal(id2))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("DeleteAlarmSubscription", func() {
		When("subscription exists", func() {
			It("deletes subscription successfully", func() {
				id := uuid.New()

				mock.ExpectExec(fmt.Sprintf("DELETE FROM %s WHERE", models.AlarmSubscription{}.TableName())).
					WithArgs(id).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))

				count, err := repo.DeleteAlarmSubscription(ctx, id)
				Expect(err).ToNot(HaveOccurred())
				Expect(count).To(Equal(int64(1)))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("CreateAlarmSubscription", func() {
		It("creates new subscription successfully", func() {
			f := api.AlarmSubscriptionInfoFilterACKNOWLEDGE
			id := uuid.NewString()
			subscription := models.AlarmSubscription{
				ConsumerSubscriptionID: &id,
				Callback:               "http://test.com/callback",
				Filter:                 &f,
				EventCursor:            int64(10),
			}

			mock.ExpectQuery(fmt.Sprintf("INSERT INTO %s", models.AlarmSubscription{}.TableName())).
				WithArgs(
					subscription.Callback, subscription.ConsumerSubscriptionID, subscription.EventCursor, subscription.Filter,
				).
				WillReturnRows(pgxmock.NewRows([]string{"updated_at", "subscription_id", "consumer_subscription_id", "filter", "callback", "event_cursor", "created_at"}).
					AddRow(time.Now(), uuid.New(), &id, &f, "http://test.com/callback", int64(10), time.Now()))

			result, err := repo.CreateAlarmSubscription(ctx, subscription)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.ConsumerSubscriptionID).To(Equal(subscription.ConsumerSubscriptionID))
			Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
		})

		It("includes event_filter in the insert when set", func() {
			// When eventFilter has a value, event_filter and its enum arg appear in the
			// generated INSERT (args are ordered by sorted struct field name: Callback,
			// ConsumerSubscriptionID, EventCursor, EventFilter, Filter).
			f := api.AlarmSubscriptionInfoFilterNEW
			ef := api.AlarmSubscriptionInfoEventFilterCHANGE
			subscription := models.AlarmSubscription{
				Callback:    "http://test.com/callback",
				Filter:      &f,
				EventFilter: &ef,
				EventCursor: int64(0),
			}

			// Spot-check that event_filter is present in the INSERT column list so a dropped
			// event_filter column would fail the match rather than slip through a table-name prefix.
			mock.ExpectQuery(`(?s)INSERT INTO alarm_subscription_info.*event_filter`).
				WithArgs(
					subscription.Callback, subscription.EventCursor, subscription.EventFilter, subscription.Filter,
				).
				WillReturnRows(pgxmock.NewRows([]string{"updated_at", "subscription_id", "filter", "event_filter", "callback", "event_cursor", "created_at"}).
					AddRow(time.Now(), uuid.New(), &f, &ef, subscription.Callback, int64(0), time.Now()))

			result, err := repo.CreateAlarmSubscription(ctx, subscription)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.EventFilter).ToNot(BeNil())
			Expect(*result.EventFilter).To(Equal(ef))
			Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
		})

		It("omits event_filter from the insert when nil", func() {
			// An omitted/null eventFilter yields a nil model pointer, which GetColumnsAndValues
			// skips, so event_filter is absent from the INSERT and the column defaults to SQL NULL.
			subscription := models.AlarmSubscription{
				Callback:    "http://test.com/callback2",
				EventCursor: int64(0),
			}

			mock.ExpectQuery(fmt.Sprintf("INSERT INTO %s", models.AlarmSubscription{}.TableName())).
				WithArgs(
					subscription.Callback, subscription.EventCursor,
				).
				WillReturnRows(pgxmock.NewRows([]string{"updated_at", "subscription_id", "event_filter", "callback", "event_cursor", "created_at"}).
					AddRow(time.Now(), uuid.New(), nil, subscription.Callback, int64(0), time.Now()))

			result, err := repo.CreateAlarmSubscription(ctx, subscription)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.EventFilter).To(BeNil())
			Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
		})
	})

	Describe("PatchAlarmSubscription", func() {
		subTable := models.AlarmSubscription{}.TableName()

		When("clearing event_filter with a null value", func() {
			It("emits the column with a NULL arg and returns the updated row", func() {
				id := uuid.New()
				// A nil value in the fields map emits SET event_filter = NULL.
				// Spot-check that event_filter is the column being SET (not just any UPDATE
				// on the table), so a wrong column name would fail the match.
				mock.ExpectQuery(`(?s)UPDATE alarm_subscription_info SET.*event_filter`).
					WithArgs(nil, id).
					WillReturnRows(pgxmock.NewRows([]string{"subscription_id", "event_filter", "callback"}).
						AddRow(id, nil, "http://x"))

				result, err := repo.PatchAlarmSubscription(ctx, id, map[string]any{"EventFilter": nil})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.EventFilter).To(BeNil())
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("setting multiple columns", func() {
			It("emits only the present columns in sorted field order", func() {
				id := uuid.New()
				consumer := "consumer-123"
				ef := api.AlarmSubscriptionInfoEventFilterCHANGE
				// Fields are keyed by model field name and emitted in sorted order:
				// ConsumerSubscriptionID (consumer_subscription_id) then EventFilter (event_filter),
				// followed by the WHERE arg.
				// Spot-check both SET columns appear in sorted order (consumer_subscription_id
				// then event_filter), so a dropped column would fail the match.
				mock.ExpectQuery(`(?s)UPDATE alarm_subscription_info SET.*consumer_subscription_id.*event_filter`).
					WithArgs("consumer-123", "CHANGE", id).
					WillReturnRows(pgxmock.NewRows([]string{"subscription_id", "consumer_subscription_id", "event_filter", "callback"}).
						AddRow(id, &consumer, &ef, "http://x"))

				result, err := repo.PatchAlarmSubscription(ctx, id, map[string]any{
					"ConsumerSubscriptionID": "consumer-123",
					"EventFilter":            "CHANGE",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.ConsumerSubscriptionID).ToNot(BeNil())
				Expect(*result.ConsumerSubscriptionID).To(Equal("consumer-123"))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("the subscription does not exist", func() {
			It("returns ErrNotFound", func() {
				id := uuid.New()
				mock.ExpectQuery(fmt.Sprintf("UPDATE %s SET", subTable)).
					WithArgs("http://new", id).
					WillReturnRows(pgxmock.NewRows([]string{"subscription_id", "callback"}))

				_, err := repo.PatchAlarmSubscription(ctx, id, map[string]any{"Callback": "http://new"})
				Expect(err).To(MatchError(svcutils.ErrNotFound))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("a non-patchable field is supplied", func() {
			It("rejects the patch without touching the database", func() {
				id := uuid.New()
				// event_cursor is a valid model/db field but internal; the repository must
				// reject it before building or executing any query, independently of the
				// handler's own field filtering. No mock expectation is registered.
				_, err := repo.PatchAlarmSubscription(ctx, id, map[string]any{"EventCursor": int64(5)})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not patchable"))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})

	Describe("DeleteResolvedAlarmEventsBefore", func() {
		aerTable := models.AlarmEventRecord{}.TableName()

		When("the advisory lock is acquired", func() {
			It("deletes resolved events older than the retention period and reports it ran", func() {
				mock.ExpectBegin()
				// The cleanup is guarded by a transaction-scoped advisory lock so that
				// concurrent replicas don't run it at the same time.
				mock.ExpectQuery("pg_try_advisory_xact_lock").
					WithArgs(pgxmock.AnyArg()).
					WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(true))
				mock.ExpectExec(fmt.Sprintf("DELETE FROM %s WHERE", aerTable)).
					WillReturnResult(pgxmock.NewResult("DELETE", 5))
				mock.ExpectCommit()
				mock.ExpectRollback()

				deleted, ran, err := repo.DeleteResolvedAlarmEventsBefore(ctx, 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(ran).To(BeTrue())
				Expect(deleted).To(Equal(int64(5)))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("another replica holds the advisory lock", func() {
			It("does not delete anything and reports it did not run", func() {
				mock.ExpectBegin()
				mock.ExpectQuery("pg_try_advisory_xact_lock").
					WithArgs(pgxmock.AnyArg()).
					WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(false))
				// No DELETE is expected when the lock is not acquired.
				mock.ExpectCommit()
				mock.ExpectRollback()

				deleted, ran, err := repo.DeleteResolvedAlarmEventsBefore(ctx, 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(ran).To(BeFalse())
				Expect(deleted).To(Equal(int64(0)))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})

		When("the retention period is not positive", func() {
			It("returns an error without touching the database", func() {
				// A zero/negative retention would delete every resolved event, so it is
				// rejected before any transaction is started.
				deleted, ran, err := repo.DeleteResolvedAlarmEventsBefore(ctx, 0)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid retention period"))
				Expect(ran).To(BeFalse())
				Expect(deleted).To(Equal(int64(0)))
				Expect(mock.ExpectationsWereMet()).ToNot(HaveOccurred())
			})
		})
	})
})
