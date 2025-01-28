package repo_test

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pashagolub/pgxmock/v4"

	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
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
				Expect(err).NotTo(HaveOccurred())
				Expect(records).To(HaveLen(2))
				Expect(records[0].AlarmEventRecordID).To(Equal(id1))
				Expect(records[1].AlarmEventRecordID).To(Equal(id2))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(err).NotTo(HaveOccurred())
				Expect(result.AlarmAcknowledged).To(BeTrue())
				Expect(result.AlarmAcknowledgedTime).To(Equal(&now))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(err).NotTo(HaveOccurred())
				Expect(records.AlarmEventRecordID).To(Equal(id1))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})

	Describe("GetAlarmSubscription", func() {
		When("subscription exists", func() {
			It("retrieves a specific alarm subscription", func() {
				id := uuid.New()
				conId := uuid.New()
				callback := "http://example.com/callback"

				mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE", models.AlarmSubscription{}.TableName())).
					WithArgs(id).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"subscription_id", "consumer_subscription_id", "callback",
						}).AddRow(id, &conId, callback),
					)

				subscription, err := repo.GetAlarmSubscription(ctx, id)
				Expect(err).NotTo(HaveOccurred())
				Expect(subscription.SubscriptionID).To(Equal(id))
				Expect(subscription.ConsumerSubscriptionID).To(Equal(&conId))
				Expect(subscription.Callback).To(Equal(callback))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})

	Describe("UpsertAlarmEventRecord", func() {
		When("upserting a single record", func() {
			It("successfully upserts alarm event records", func() {
				id := uuid.New()
				records := []models.AlarmEventRecord{
					{
						AlarmRaisedTime:   time.Now(),
						PerceivedSeverity: api.WARNING,
						ObjectID:          &id,
						Fingerprint:       "9a9e2d82a78cf2b9",
					},
				}

				mock.ExpectExec(fmt.Sprintf("INSERT INTO %s", models.AlarmEventRecord{}.TableName())).
					WithArgs(
						records[0].AlarmRaisedTime, records[0].AlarmClearedTime,
						records[0].AlarmAcknowledgedTime, records[0].AlarmAcknowledged,
						records[0].PerceivedSeverity, records[0].Extensions,
						records[0].ObjectID, records[0].ObjectTypeID,
						records[0].AlarmStatus, records[0].Fingerprint,
						records[0].AlarmDefinitionID, records[0].ProbableCauseID,
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				err := repo.UpsertAlarmEventRecord(ctx, records)
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
					},
					{
						AlarmRaisedTime:   now,
						PerceivedSeverity: api.CRITICAL,
						ObjectID:          &id2,
						Fingerprint:       "9a9e2d82a78cf2b9",
					},
				}

				mock.ExpectExec(fmt.Sprintf("INSERT INTO %s", models.AlarmEventRecord{}.TableName())).
					WithArgs(
						records[0].AlarmRaisedTime, records[0].AlarmClearedTime,
						records[0].AlarmAcknowledgedTime, records[0].AlarmAcknowledged,
						records[0].PerceivedSeverity, records[0].Extensions,
						records[0].ObjectID, records[0].ObjectTypeID,
						records[0].AlarmStatus, records[0].Fingerprint,
						records[0].AlarmDefinitionID, records[0].ProbableCauseID,
						records[1].AlarmRaisedTime, records[1].AlarmClearedTime,
						records[1].AlarmAcknowledgedTime, records[1].AlarmAcknowledged,
						records[1].PerceivedSeverity, records[1].Extensions,
						records[1].ObjectID, records[1].ObjectTypeID,
						records[1].AlarmStatus, records[1].Fingerprint,
						records[1].AlarmDefinitionID, records[1].ProbableCauseID,
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 2))

				err := repo.UpsertAlarmEventRecord(ctx, records)
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})

		When("given an empty record list", func() {
			It("handles empty record list", func() {
				err := repo.UpsertAlarmEventRecord(ctx, []models.AlarmEventRecord{})
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})

	Describe("GetAlarmsForSubscription", func() {
		It("retrieves alarms based on subscription criteria", func() {
			f := api.AlarmSubscriptionInfoFilterNEW
			subscription := models.AlarmSubscription{
				SubscriptionID: uuid.New(),
				EventCursor:    5,
				Filter:         &f,
			}

			mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE", models.AlarmEventRecord{}.TableName())).
				WithArgs(subscription.EventCursor, subscription.Filter).
				WillReturnRows(
					pgxmock.NewRows([]string{
						"alarm_event_record_id", "alarm_raised_time",
						"perceived_severity", "notification_event_type",
					}).
						AddRow(uuid.New(), time.Now(), api.WARNING, f),
				)

			results, err := repo.GetAlarmsForSubscription(ctx, subscription)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
		})

		It("filters alarms by notification event type", func() {
			f := api.AlarmSubscriptionInfoFilterACKNOWLEDGE
			subscription := models.AlarmSubscription{
				SubscriptionID: uuid.New(),
				EventCursor:    5,
				Filter:         &f,
			}

			mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE", models.AlarmEventRecord{}.TableName())).
				WithArgs(subscription.EventCursor, subscription.Filter).
				WillReturnRows(
					pgxmock.NewRows([]string{
						"alarm_event_record_id", "alarm_raised_time",
						"perceived_severity", "notification_event_type",
						"alarm_sequence_number",
					}).AddRow(uuid.New(), time.Now(), api.WARNING, f, int64(6)))

			results, err := repo.GetAlarmsForSubscription(ctx, subscription)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].NotificationEventType).To(Equal(f))
			Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
		})

		It("handles subscription with no filter", func() {
			subscription := models.AlarmSubscription{
				SubscriptionID: uuid.New(),
				EventCursor:    5,
				Filter:         nil,
			}

			mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE", models.AlarmEventRecord{}.TableName())).
				WithArgs(subscription.EventCursor).
				WillReturnRows(pgxmock.NewRows([]string{
					"alarm_event_record_id", "alarm_raised_time",
					"perceived_severity", "notification_event_type",
					"alarm_sequence_number",
				}))

			results, err := repo.GetAlarmsForSubscription(ctx, subscription)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
		})

		It("handles no alarms above event cursor", func() {
			subscription := models.AlarmSubscription{
				SubscriptionID: uuid.New(),
				EventCursor:    1000, // High cursor value
				Filter:         nil,
			}

			mock.ExpectQuery(fmt.Sprintf("SELECT (.+) FROM %s WHERE", models.AlarmEventRecord{}.TableName())).
				WithArgs(subscription.EventCursor).
				WillReturnRows(pgxmock.NewRows([]string{
					"alarm_event_record_id", "alarm_raised_time",
					"perceived_severity", "notification_event_type",
					"alarm_sequence_number",
				}))

			results, err := repo.GetAlarmsForSubscription(ctx, subscription)
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})

	Describe("ResolveNotificationIfNotInCurrent", func() {
		When("resolving notifications", func() {
			It("resolves notifications not in current payload", func() {
				clearTime := time.Now()
				alarmsrepo.TimeNow = func() time.Time {
					return clearTime
				}
				fp := "9a9e2d82a78cf2b9" //nolint:goconst
				t := time.Now()
				am := &api.AlertmanagerNotification{
					Alerts: []api.Alert{
						{
							Fingerprint: &fp,
							StartsAt:    &t,
						},
					},
				}
				mock.ExpectQuery(fmt.Sprintf("UPDATE %s SET", models.AlarmEventRecord{}.TableName())).
					WithArgs(api.Resolved, clearTime, api.CLEARED, &fp, &t).
					WillReturnRows(pgxmock.NewRows([]string{"alarm_event_record_id"}).AddRow(uuid.New()))

				err := repo.ResolveNotificationIfNotInCurrent(ctx, am)
				Expect(err).NotTo(HaveOccurred())
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(err).NotTo(HaveOccurred())
				Expect(records).To(HaveLen(1))
				Expect(records[0].ID).To(Equal(id1))
				Expect(records[0].RetentionPeriod).To(Equal(s.RetentionPeriod))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(err).NotTo(HaveOccurred())
				Expect(record.ID).To(Equal(s.ID))
				Expect(record.RetentionPeriod).To(Equal(s.RetentionPeriod))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})

	Describe("GetAlarmSubscriptions", func() {
		When("records exist", func() {
			It("returns all alarms subscription", func() {
				id1, id2 := uuid.New(), uuid.New()
				conID1, conID2 := uuid.New(), uuid.New()
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
				Expect(err).NotTo(HaveOccurred())
				Expect(records).To(HaveLen(2))
				Expect(records[0].SubscriptionID).To(Equal(id1))
				Expect(records[1].SubscriptionID).To(Equal(id2))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
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
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(int64(1)))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})

	Describe("CreateAlarmSubscription", func() {
		It("creates new subscription successfully", func() {
			f := api.AlarmSubscriptionInfoFilterACKNOWLEDGE
			id := uuid.New()
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
			Expect(err).NotTo(HaveOccurred())
			Expect(result.ConsumerSubscriptionID).To(Equal(subscription.ConsumerSubscriptionID))
			Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
		})
	})

	Describe("GetMaxAlarmSeq", func() {
		When("alarms exist", func() {
			It("returns maximum alarm sequence number", func() {
				mock.ExpectQuery(fmt.Sprintf(`SELECT (.+MAX.+) FROM "%s"`, models.AlarmEventRecord{}.TableName())).
					WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(int64(42)))

				maxSeq, err := repo.GetMaxAlarmSeq(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(maxSeq).To(Equal(int64(42)))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})

		When("no alarms exist", func() {
			It("returns 0 when no alarms exist", func() {
				mock.ExpectQuery("SELECT COALESCE").
					WillReturnRows(pgxmock.NewRows([]string{"coalesce"}).AddRow(int64(0)))

				maxSeq, err := repo.GetMaxAlarmSeq(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(maxSeq).To(Equal(int64(0)))
				Expect(mock.ExpectationsWereMet()).NotTo(HaveOccurred())
			})
		})
	})
})
