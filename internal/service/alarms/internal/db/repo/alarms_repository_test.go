package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/testhelpers"
)

var pgContainer *testhelpers.PostgresContainer

// BeforeSuite suite setup
var _ = BeforeSuite(func() {
	ctx := context.Background()

	var err error
	pgContainer, err = testhelpers.CreatePostgresContainer(ctx)
	Expect(err).ShouldNot(HaveOccurred())
	Expect(pgContainer).ShouldNot(BeNil())

})

// AfterSuite suite teardown
var _ = AfterSuite(func() {
	if pgContainer != nil {
		_ = pgContainer.Terminate(context.Background())
	}
})

var _ = Describe("PostgreSQL Integration Test For Alarms repository", func() {
	var pool *pgxpool.Pool
	var migrationHandler *db.MigrationHandler
	BeforeEach(func() {
		var err error
		pool, err = db.NewPgxPool(context.Background(), pgContainer.PgConfig)
		Expect(err).ShouldNot(HaveOccurred())

		// Apply migration
		migrationHandler, err = db.NewHandler(db.PGtoMigrateConfig(pgContainer.PgConfig))
		Expect(err).ShouldNot(HaveOccurred())
		err = migrationHandler.Migrate.Up()
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		// Apply down migration
		err := migrationHandler.Migrate.Down()
		Expect(err).ShouldNot(HaveOccurred())

		// Close db
		if pool != nil {
			pool.Close()
		}
	})

	It("should not be able return any aer model since it doesnt exist", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		alarmsRepository := AlarmsRepository{Db: pool}
		aer, err := alarmsRepository.GetAlarmEventRecordWithUuid(ctx, uuid.MustParse("6163a236-fd22-4942-9e77-bbebb822e209"))

		Expect(err).ShouldNot(HaveOccurred())
		Expect(aer).Should(HaveLen(0))
	})

	It("should return exactly one entry of aer model", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		alarmsRepository := AlarmsRepository{Db: pool}
		idToTest := "c1a66f8c-8454-4e3b-8d8d-9cbe119f200a"
		insertTestData(pool)
		aer, err := alarmsRepository.GetAlarmEventRecordWithUuid(ctx, uuid.MustParse(idToTest))

		Expect(err).ShouldNot(HaveOccurred())
		Expect(aer).ShouldNot(BeNil())
		Expect(aer[0].AlarmEventRecordID).Should(Equal(uuid.MustParse(idToTest)))
	})
})

func insertTestData(pool *pgxpool.Pool) {
	seedSQL := `
INSERT INTO alarm_dictionary(alarm_dictionary_id, resource_type_id, alarm_dictionary_version, entity_type, vendor)
VALUES ('c7e5e1e6-6f16-4b9a-807f-c18088def392','23f07ebd-2bc1-43eb-8c00-0a733949cb1d', '4.16', 'telco-model-OpenShift-4.16.2', 'Red Hat');


INSERT INTO alarm_definition (alarm_definition_id, resource_type_id, probable_cause_id, alarm_name, alarm_last_change, alarm_description, proposed_repair_actions, alarm_dictionary_id)
VALUES ('c7e5e1e6-6f16-4b9a-807f-c18088def392', '23f07ebd-2bc1-43eb-8c00-0a733949cb1d', '2266f5fd-2dca-47bf-a701-802c463162c8',
        'NodeWithoutOVNKubeNodePodRunning','4.16',
        'All Linux nodes should be running an ovnkube-node pod, {{ $labels.node }} is not.',
        'https://github.com/openshift/runbooks/blob/master/alerts/cluster-network-operator/NodeWithoutOVNKubeNodePodRunning.md', 'c7e5e1e6-6f16-4b9a-807f-c18088def392');

INSERT INTO alarm_event_record (alarm_event_record_id, alarm_definition_id, probable_cause_id, alarm_raised_time, perceived_severity, resource_id, resource_type_id, notification_event_type, fingerprint)
VALUES ('c1a66f8c-8454-4e3b-8d8d-9cbe119f200a', 'c7e5e1e6-6f16-4b9a-807f-c18088def392', '2266f5fd-2dca-47bf-a701-802c463162c8',
        CURRENT_TIMESTAMP, 3, '550e8400-e29b-41d4-a716-446655440000', '550e8400-e29b-41d4-a716-446655440000',1,'asd123');
`
	// Execute the seed SQL
	_, err := pool.Exec(context.Background(), seedSQL)
	Expect(err).ShouldNot(HaveOccurred())
}
