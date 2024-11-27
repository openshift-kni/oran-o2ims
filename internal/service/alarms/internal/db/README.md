# DB migration

## Migration

- Library and CLI: <https://github.com/golang-migrate/migrate>
- More on migration [here](https://github.com/golang-migrate/migrate/blob/c378583d782e026f472dff657bfd088bf2510038/MIGRATIONS.md)
- Tutorial [here](https://github.com/golang-migrate/migrate/blob/c378583d782e026f472dff657bfd088bf2510038/database/postgres/TUTORIAL.md)

## Library

Call to run migration all the way up

```shell
make run-alarms-migrate
```

This bit will later be integrated into a k8s job before the actual server is deployed.

## CLI tips

### Create a new migration file

This will create a new *up and a corresponding* down file.

```shell
migrate create -ext sql -dir internal/service/alarms/internal/db/migrations -seq create_alarm_dictionary_table
```

### Recover to a good state

Migration `Up` call may fail for whatever reason (e.g bad syntax). A failed migration is something we need to tend to manually
as it may otherwise hide bigger issues. (TODO: )

And example walk through maybe like this -

1. Migration 3 failed with the follow error

   ```shell
   ...
   2024/11/05 14:30:31 INFO Start buffering 3/u create_alarm_event_record_table
   2024/11/05 14:30:31 INFO Read and execute 3/u create_alarm_event_record_table
   2024/11/05 14:30:31 ERROR failed to do migration err="failed to run migrations: run migrations: migration failed: column \"status\" does not exist (column 34) in line 36: -- 
                          Counter to keep track of the latest events, used to notify only the latest event.\nCREATE SEQUENCE IF NOT EXISTS alarm_sequence_seq\n...
   ```

   At this point even if you rerun the migration code it will be blocked

   ```shell
   make run-alarms-migrate
   ...
   2024/11/05 14:31:07 ERROR failed to do migration err="failed to run migrations: run migrations: Dirty database version 3. Fix and force version."
   ```

2. Fix the migration file as needed. In this example a variable was referenced incorrectly.
3. Force the `migration_table` (a table that the migration lib creates and uses to keep track of state) to a known good number using CLI. In this case it would be 2.

   ```shell
   migrate -source=file://internal/service/alarms/internal/db/migrations -database="postgres://alarms:alarms@localhost:5432/alarms?sslmode=disable&x-migrations-table=schema_migrations" force 2
   ```

4. Rerun `make run-alarms-migrate`

### Full DB cleanup

```shell
 migrate -source=file://internal/service/alarms/internal/db/migrations -database="postgres://alarms:alarms@localhost:5432/alarms?sslmode=disable&x-migrations-table=schema_migrations" down                                                                                                  
Are you sure you want to apply all down migrations? [y/N]
y
Applying all down migrations
3/d create_alarm_event_record_table (18.00275ms)
2/d create_alarm_defination_table (28.736875ms)
1/d create_alarm_dictionary_table (34.635958ms)
```
