# Lifecycle of infrastructure monitoring alarms api

```yaml
title: lifecycle-of-infrastructure-monitoring-alarms-api
authors:
  - @browsell
  - @Jennifer-chen-rh
  - @pixelsoccupied
reviewers: # Include a comment about what domain expertise a reviewer is expected to bring and what area of the enhancement you expect them to focus on. For example: - "@networkguru, for networking aspects, please look at IP bootstrapping aspect"
  - @alegacy
  - @bartwensley
  - @browsell
  - @edcdavid
  - @Jennifer-chen-rh
approvers: # A single approver is preferred, the role of the approver is to raise important questions, help ensure the enhancement receives reviews from all applicable areas/SMEs, and determine when consensus is achieved such that the EP can move forward to implementation.  Having multiple approvers makes it difficult to determine who is responsible for the actual approval.
  - @browsell
api-approvers: # In case of new or modified APIs or API extensions (CRDs, aggregated apiservers, webhooks, finalizers). If there is no API change, use "None"
  - TBD
creation-date: 2024-09-26
last-updated: 2024-10-09
tracking-link: # link to the tracking ticket (for example: Jira Feature or Epic ticket) that corresponds to this enhancement
  - CNF-13185
see-also:
  - "None"
replaces:
  - "None"
superseded-by:
  - "None"
```

## Table of Contents

- [Summary](#Summary)
- [Goals](#Goals)
- [Key O-RAN data structures](#key-O-RAN-data-structures)
- [InfrastructureMonitoring Service API](#Infrastructure-Monitoring-Service-Alarms-API)
- [Database schema](#schema)
- [Init behaviour](#init)
  - [Detailed Steps](#detailed-server-instantiation)
- [Ready behaviour](#ready)
  - [Find AlarmDefinitionID and ProbableCauseID from current Alerts](#for-a-given-resourcetypeid-and-alarmname-coming-from-am-alert-find-the-alarmdefinitionid-and-probablecauseid)
  - [Notification tracking](#notification-tracking)
  - [Cleaning historical data](#daily-archive-cleanup)
  - [Get ProbableCause ID, name and description](#get-probable-cause-id-name-and-description)
- [Kubernetes](#k8s-resources)
- [Tooling](#tooling-and-general-dev-guidelines)
- [Future Updates](#future-updates)

## Summary

`O-RAN` requires `InfrastructureMonitoring Service Alarms API` which is a collection of APIs that can be queried by client to
monitor the health of the `o-cloud`. This enhancement describes initialization steps and ready steps for `InfrastructureMonitoring Service Alarms API`
as well everything that's needed to fully develop the app.

At a high level, this service can be viewed as a thin wrapper of ACM observability stack which translates
OCP cluster resources to data structures defined by `O-RAN` spec. Among other things
the service exposes APIs, configures Alertmanager deployment, read PrometheusRules from managedclusters and finally
store data in a persistent storage.

### Goals

- Define steps to initialize and for when ready serve API calls
- Define database schema
- Define K8s CRs
- Define developer tools
- Allow future integration of alarms from additional sources (e.g H/W)

## Key O-RAN data structures

`InfrastructureMonitoring Service API Alarms`, primarily deals with the following O-RAN data structures during initialization.
Comments for each attribute is taken from O-RAN spec doc.

Please note that this is not an exhaustive list but are here to help the reader get a feel for the Alarm specific data we are dealing with.
See the official doc `O-RAN.WG6.O2IMS-INTERFACE-R003-v06.00 (June 2024)` (download from [here](https://specifications.o-ran.org/download?id=674)) for more.

- AlarmDictionary
    This is primarily the link between Alarms and Inventory. A ResourceType which is currently a "NodeCluster" (Kind: Logical and Class: Compute)
    can have exactly one AlarmDictionary. Each dictionary instance will have version that corresponds to the major.minor
    version of the ResourceType. This version is later used to find an AlarmDefinition given an alert name and ResourceTypeID.

    ```go
    type AlarmDictionary struct {
        AlarmDictionaryVersion       string                  `json:"alarmDictionaryVersion"`       // M, 1, Version of the Alarm Dictionary. Version is vendor defined such that the version of the dictionary can be associated with a specific version of the software delivery of this product.
        AlarmDictionarySchemaVersion string                  `json:"alarmDictionarySchemaVersion"` // M, 1, Version of the Alarm Dictionary Schema to which this alarm dictionary conforms. Note: The specific value for this should be defined in the IM/DM specification for the Alarm Dictionary Model Schema when it is published at a future date
        EntityType                   string                  `json:"entityType"`                   // M, 1, O-RAN entity type emitting the alarm:  This shall be unique per vendor ResourceType.model and ResourceType.version
        Vendor                       string                  `json:"vendor"`                       // M, 1, Vendor of the Entity Type to whom this Alarm Dictionary applies. This should be the same value as in the ResourceType.vendor attribute.
        ManagementInterfaceID        []ManagementInterfaceID `json:"managementInterfaceId"`        // M, 1..N, List of management interface over which alarms are transmitted for this Entity Type. RESTRICTION: For the O-Cloud IMS Services this value is limited to O2IMS.
        PKNotificationField          []string                `json:"pkNotificationField"`          // M, 1..N, Identifies which field or list of fields in the alarm notification contains the primary key (PK) into the Alarm Dictionary for this interface; i.e. which field contains the Alarm Definition ID.
        AlarmDefinition              []AlarmDefinition       `json:"alarmDefinition"`              // M, 1..N, List of alarms that can be detected against this ResourceType
    }
    ```

- AlarmDefinition

    AlarmDefinition is what stores rules that are always evaluated to see if an alert is fired by a ResourceType. For caas, this is effectively the content of `PrometheusRules`.

    ```go
    type AlarmDefinition struct {
        AlarmDefinitionID     uuid.UUID               `json:"alarmDefinitionID"`               // M, 1, Provides a unique identifier of the alarm being raised.  This is the Primary Key into the Alarm Dictionary.
        AlarmName             string                  `json:"alarmName"`                       // M, 1, Provides short name for the alarm.
        AlarmLastChange       string                  `json:"alarmLastChange"`                 // M, 1, Indicates the Alarm Dictionary Version in which this alarm last changed.
        AlarmChangeType       AlarmLastChangeType     `json:"alarmChangeType"`                 // M, 1, Indicates the type of change that occurred during the alarm last change; added, deleted, modified.
        AlarmDescription      string                  `json:"alarmDescription"`                // M, 1, Provides a longer descriptive meaning of the alarm condition and a description of the consequences of the alarm condition. This is intended to be read by an operator to give an idea of what happened and a sense of the effects, consequences, and other impacted areas of the system
        ProposedRepairActions string                  `json:"proposedRepairActions"`           // M, 1, Provides guidance for proposed repair actions.
        ClearingType          ClearingType            `json:"clearingType"`                    // M, 1, Whether alarm is cleared automatically or manually
        ManagementInterfaceID []ManagementInterfaceID `json:"managementInterfaceId,omitempty"` // M, 0..N, List of management interface over which alarms are transmitted for this Entity Type. RESTRICTION: For the O-Cloud IMS Services this value is limited to O2IMS.
        PKNotificationField   []string                `json:"pkNotificationField,omitempty"`   // M, 0..N, Identifies which field or list of fields in the alarm notification contains the primary key (PK) into the Alarm Dictionary for this interface; i.e. which field contains the Alarm Definition ID.
        AlarmAdditionalFields []AttributeValuePair    `json:"alarmAdditionalFields,omitempty"` // M, 0..N, List of metadata key-value pairs used to associate meaningful metadata to the related resource type.
    }
    ```

- ProbableCause

    ProbableCause is a subset of data present in AlarmDefinition

    ```go
    type ProbableCause struct {
        ProbableCauseID uuid.UUID `json:"probableCauseId"` // M, Identifier of the ProbableCause. 
        Name            string    `json:"name"`            // M, Human readable text of the probable cause.
        Description     string    `json:"description"`     // M, Any additional information beyond the name to describe the probableCause
    }
    ```

- AlarmEventRecord

    AlarmEventRecord how we represent an alert that is fired or resolved. An alert coming from Alertmanager is mapped 1:1 with an instance of AlarmEventRecord.

    ```go
    type AlarmEventRecord struct {
        AlarmEventRecordID    uuid.UUID         `json:"alarmEventRecordId"`              // M, Identifier of an entry in the AlarmEventRecord. Locally unique within the scope of an O-Cloud instance.
        ResourceID            uuid.UUID         `json:"resourceId"`                      // M, A reference to the resource instance which caused the alarm.
        AlarmDefinitionID     uuid.UUID         `json:"alarmDefinitionId"`               // M, A reference to the Alarm Definition record in the Alarm Dictionary associated with the referenced Resource Type.
        ProbableCauseID       uuid.UUID         `json:"probableCauseId"`                 // M, A reference to the ProbableCause of the Alarm.
        AlarmRaisedTime       time.Time         `json:"alarmRaisedTime"`                 // M, This field is populated with a Date/Time stamp value when the AlarmEventRecord is created.
        AlarmChangedTime      *time.Time        `json:"alarmChangedTime,omitempty"`      // M, This field is populated with a Date/Time stamp value when any value of the AlarmEventRecord is modified.
        AlarmClearedTime      *time.Time        `json:"alarmClearedTime,omitempty"`      // M, This field is populated with a Date/Time stamp value when the alarm condition is cleared.
        AlarmAcknowledgedTime *time.Time        `json:"alarmAcknowledgedTime,omitempty"` // M, This field is populated with a Date/Time stamp value when the alarm condition is acknowledged.
        AlarmAcknowledged     bool              `json:"alarmAcknowledged"`               // M, This is a Boolean value defaulted to FALSE. When a system acknowledges an alarm, it is then set to TRUE.
        PerceivedSeverity     PerceivedSeverity `json:"perceivedSeverity"`               // M, This is an enumerated set of values which identify the perceived severity of the alarm.
        Extensions            []KeyValue        `json:"extensions"`                      // M, These are unspecified (not standardized) properties (keys) which are tailored by the vendor or operator to extend the information provided about the O-Cloud Alarm.
    }
    ```

- AlarmSubscriptionInfo
   This what stores info about subscription who needs to be notified when an Alarm is raised

   3.3.6.2.3

    ```go
    type AlarmSubscriptionInfo struct {
        SubscriptionID              uuid.UUID `json:"subscriptionID"`         // M, Identifier of the subscription. Locally unique within the scope of an O-Cloud instance.
        ConsumerSubscriptionID      uuid.UUID `json:"consumerSubscriptionId"` // O, The consumer may provide its identifier for tracking, routing, or identifying the subscription used to report the event.
        Filter                      string    `json:"filter"`                 // O, Criteria for events which do not need to be reported or will be filtered by the subscription notification service. Therefore, if a filter is not provided then all events are reported.
        Callback                    string    `json:"callback"`               // M, The fully qualified URI to a consumer procedure which can process a Post of the AlarmEventNotification.
    }
    ```

## Infrastructure Monitoring Service Alarms API

See the official doc `O-RAN.WG6.O2IMS-INTERFACE-R003-v06.00 (June 2024)` (download from [here](https://specifications.o-ran.org/download?id=674)) to check APIs that we need to expose.

| **Endpoint**                                                                  | **HTTP Method** | **Description**                                                     | **Input Payload**                                                            | **Returned Data**                   |
|-------------------------------------------------------------------------------|-----------------|---------------------------------------------------------------------|------------------------------------------------------------------------------|-------------------------------------|
| `/O2ims_infrastructureMonitoring/v1/alarms`                                   | GET             | Retrieve the list of alarms.                                        | Optional query parameters `filter`                                           | A list of `AlarmEventRecord`        |
| `/O2ims_infrastructureMonitoring/v1/alarms/{alarmEventRecordId}`              | GET             | Retrieve exactly one alarm identified by `alarmEventRecordId`.      | None                                                                         | Exactly one `AlarmEventRecord`      |
| `/O2ims_infrastructureMonitoring/v1/alarms/{alarmEventRecordId}`              | PATCH           | Modify exactly one alarm identified by `alarmEventRecordId` to ack. | `AlarmEventRecordModifications`(no perceivedSeverity only alarmAcknowledged) | `AlarmEventRecordModifications`     |
| `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions`                       | GET             | Retrieve the list of alarm subscriptions.                           | Optional query parameters `filter`                                           | A list of `AlarmSubscriptionInfo`   |
| `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions`                       | POST            | Create a new alarm subscriptions.                                   | `AlarmSubscriptionInfo`                                                      | Exactly one `AlarmSubscriptionInfo` |
| `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions/{alarmSubscriptionId}` | GET             | Retrieve exactly one subscription using `alarmSubscriptionId`.      | None                                                                         | Exactly one `AlarmSubscriptionInfo` |
| `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions/{alarmSubscriptionId}` | DELETE          | Delete exactly one subscription using `alarmSubscriptionId`.        | None                                                                         | None                                |

| **Endpoint for clients but not currently in spec**                    | **HTTP Method** | **Description**                                              | **Input Payload** | **Returned Data**           |
|-----------------------------------------------------------------------|-----------------|--------------------------------------------------------------|-------------------|-----------------------------|
| `/O2ims_infrastructureMonitoring/v1/probableCauses`                   | GET             | Retrieve all probable causes                                 | None              | A list of `ProbableCause`   |
| `/O2ims_infrastructureMonitoring/v1/probableCauses/{probableCauseId}` | GET             | Retrieve exactly one probable cause using `probableCauseId`. | None              | Exactly one `ProbableCause` |

| **Internal Endpoint**                           | **HTTP Method** | **Description**                              | **Input Payload**                                                                                | **Returned Data** |
|-------------------------------------------------|-----------------|----------------------------------------------|--------------------------------------------------------------------------------------------------|-------------------|
| `/internal/v1/caas-alerts/alertmanager`         | POST            | Alertmanager notifications come through here | Payload defined [here](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config) | None              |
| `/internal/v1/hardware-alerts/{hw-vendor-name}` | POST            | TBD                                          | TBD                                                                                              | TBD               |

### `alarms` family

#### Steps for `/O2ims_infrastructureMonitoring/v1/alarms` with GET

1. Get all alarms from `alarm_event_record` table (optionally using `?filter` param values)
2. Response with retrieved list of AlarmEventRecord and appropriate code

#### Steps for `/O2ims_infrastructureMonitoring/v1/alarms/{alarmEventRecordId}` with GET

1. Client calls with an AlarmEventRecordID
2. Search in both `alarm_event_record` and `alarm_event_record_archive` with `AlarmEventRecordID`
3. Response with retrieved instance of AlarmEventRecord and appropriate code

#### Steps for `/O2ims_infrastructureMonitoring/v1/alarms/{alarmEventRecordId}` with PATCH

1. Client calls with an AlarmEventRecordID and `AlarmEventRecordModifications` as patch payload
2. If `AlarmEventRecordModifications.alarmAcknowledged` is True, update `alarm_event_record` table
   - Note: `alarmAcknowledged` will be updated to `false` if `alarm_event_record.alarm_changed_time` changes (TODO: update DB to auto handle this)
3. Response with `AlarmEventRecordModifications` and appropriate code

### `alarmSubscriptions` family

#### Steps for `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions` with GET

1. Query the storage `alarm_subscription_info` (optionally using `?filter` param values)
2. Response with a list of `AlarmSubscriptionInfo` and appropriate code

#### Steps for `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions` with POST

1. Client calls with `AlarmSubscriptionInfo` payload
2. Validate the filter (e.g check if the columns actually exist)
3. Insert `alarm_subscription_info`, for now we limit to 5 (if already 5, return with an error).
4. Response with `AlarmSubscriptionInfo` and appropriate code

#### Steps for `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions/{alarmSubscriptionId}` with GET

1. Client calls with an `alarmSubscriptionId`
2. Query the storage `alarm_subscription_info` table using `alarmSubscriptionId`
3. Response with retrieved instance of `AlarmSubscriptionInfo` and appropriate code

#### Steps for `/O2ims_infrastructureMonitoring/v1/alarmSubscriptions/{alarmSubscriptionId}` with DELETE

1. Client calls with an `alarmSubscriptionId`
2. Delete the row `alarm_subscription_info` using `alarmSubscriptionId`
3. No special response (only appropriate code)

### `probableCause` family

#### Steps for `/O2ims_infrastructureMonitoring/v1/probableCause` with GET

1. Query the tables `probable_causes` and `alarm_definitions` to fetch probableCause ID, alarm name and alarm description
2. Response with a list of `probableCause` and appropriate code

#### Steps for `/O2ims_infrastructureMonitoring/v1/probableCause/{probableCauseId}` with GET

1. Query the tables `probable_causes` and `alarm_definitions` using `probableCauseId` to fetch probableCause ID (should be same as probableCauseId from input ), alarm name and alarm description
2. Response with retrieved instance of `probableCause` and appropriate code

### `internal` family

NOTE: These APIs are not exposed to client and available only internally

#### Steps for `internal/v1/caas-alerts/alertmanager`

This API is activated only after configuring ACM alertmanager that can call back alarm service.

All the alerts coming through this endpoint are of ResourceType that's "Logical"(kind) and "Compute"(class) which is
effectively a cluster. For additional types of ResourceType we would like support in the future,
we can simply follow the same pattern
i.e figure out the Kind and Class to get the ID + understand the source of Definitions + potentially open up another
endpoint if alert aggregator is not Alertmanager.

A minimal configuration

```yaml
route:
  receiver: webhook_receiver
receivers:
  - name: webhook_receiver
    webhook_configs:
      - url: "alarm-server.oran-o2ims.svc.cluster.local/internal/v1/caas-alerts/alertmanager" # this will be derived from deployment
        send_resolved: true 
```

```shell
oc -n open-cluster-management-observability create secret generic alertmanager-config --from-file=alertmanager.yaml --dry-run -o=yaml |  oc -n open-cluster-management-observability replace secret --filename=-
```

1. Example payload [here](#alertmanager-example-payload)
2. Sync `alarm_event_record` Table
   - Update rows to "resolved" that are missing in the current payload (i.e previously seen but somehow missed the "resolved" notification)

   ```sql
      UPDATE alarm_event_record
      SET status = 'resolved', alarm_cleared_time = CURRENT_TIMESTAMP
      WHERE (finger_print, alarm_raised_time) NOT IN (
             ('Something-Thats-Only-Now-Available', '2023-09-01 10:02:00+00')
      );
     ```

   - Create new AlarmEventRecord. Alert entry payload will require Alarm Definition ID and Probable Cause ID which can retrieved as shown [here](#for-a-given-resourcetypeid-and-alarmname-coming-from-am-alert-find-the-alarmdefinitionid-and-probablecauseid)
   - Upsert (i.e an insert + update operation) all AlarmEventRecord all the "firing" and "resolved" alerts as indicated by `alerts[].status`  

   ```sql
      -- ...INSERT, follwed by...
      ON CONFLICT ON CONSTRAINT unique_finger_print_alarm_raised_time DO UPDATE -- defined in schema 
      SET status = EXCLUDED.status,
      alarm_cleared_time = EXCLUDED.alarm_cleared_time,
      alarm_changed_time = EXCLUDED.alarm_changed_time;
     ```

3. Grab the Subscriptions and send notification.
   - Database interaction is further explained [here](#notification-tracking)
4. Move all the `status: resolved` rows from `alarm_event_record` to `alarm_event_record_archive`

Eventually data in `alarm_event_record_archive` will be cleared (hardcoded to 24hr) as seen [here](#daily-archive-cleanup-)

## Alertmanager Example payload

```json
 "receiver": "webhook_receiver",
 "status": "firing"
 "alerts": [
      {
          "status": "firing",
          "labels": {
              "alertname": "UpdateAvailable",
              "channel": "stable-4.16",
              "managed_cluster": "89070983-a62f-4dbe-9457-7e0c27832c63",
              "namespace": "openshift-cluster-version",
              "openshift_io_alert_source": "platform",
              "prometheus": "openshift-monitoring/k8s",
              "severity": "info",
              "upstream": "<default>"
          },
          "annotations": {
              "description": "For more information refer to 'oc adm upgrade' or https://console-openshift-console.apps.cnfdf27.sno.telco5gran.eng.rdu2.redhat.com/settings/cluster/.",
              "summary": "Your upstream update recommendation service recommends you update your cluster."
          },
          "startsAt": "2024-08-28T11:39:17.958Z",
          "endsAt": "0001-01-01T00:00:00Z",
          "generatorURL": "https://console-openshift-console.apps.cnfdf27.sno.telco5gran.eng.rdu2.redhat.com/monitoring/graph?g0.expr=sum+by+%28channel%2C+namespace%2C+upstream%29+%28cluster_version_available_updates%29+%3E+0&g0.tab=1",
          "fingerprint": "91406cd113ad87e5"
      },
 ]
```

Equivalent AlarmEventRecord in Go

```go
alarmEventRecord := AlarmEventRecord{
AlarmDefinitionID: "82139b9c-3683-427b-a1f5-4bf9d6dbb0d8", // See section "For a given ResourceTypeID and AlarmName (coming from AM alert), find the AlarmDefinitionID and ProbableCauseID"
ProbableCauseID:   "7fe31219-c2a2-48c7-a2f5-f4bcfe0486e7", // See section "For a given ResourceTypeID and AlarmName (coming from AM alert), find the AlarmDefinitionID and ProbableCauseID"
AlarmEventRecordID: uuid.New(), // Auto generated with DB                 
ResourceTypeID:     "c9d3f9c5-8429-4484-8179-2a7977071bbf", // See section "Notes on Init phase" on how labels.managed_cluster mapped to resourceTypeID
ResourceID:        "d43bf16b-c9c6-432b-934e-7b670cc6a2cc", // labels.managed_cluster
AlarmRaisedTime:   "12-31-1920 12:32:14Z",                 // comes from alertmanager `startsAt`
AlarmAcknowledged: false, 
PerceivedSeverity: SeverityCritical,                      // comes from labels.severity
Extensions:        []KeyValue{{Key: "namespace", Value: "openshift-cluster-version"}, ....}, // any labels that are not processed already (e.g skip labels.severity)

// Optionally 
AlarmChangedTime         // use changedAt
AlarmClearedTime         // use endsAt or potentially current time if we missed the resolved notification
AlarmAcknowledgedTime    // Done through API request AlarmEventRecordModifications.alarmAcknowledged (update AlarmAcknowledged)
}
```

## Schema

All O-RAN services will use the same O-CLOUD DB service. More on DB deployment [here](#postgres).

Each table is modeled after O-RAN data structures. DB in our case wil be called `alarms`.
Init SQL may look like the following:

```sql
CREATE DATABASE alarms;

-- ENUM for ManagementInterfaceID
DROP TYPE IF EXISTS ManagementInterfaceID CASCADE;
CREATE TYPE ManagementInterfaceID AS ENUM ('O1', 'O2DMS', 'O2IMS', 'OpenFH');

-- ENUM for AlarmLastChangeType
DROP TYPE IF EXISTS AlarmLastChangeType CASCADE;
CREATE TYPE AlarmLastChangeType AS ENUM ('added', 'deleted', 'modified');

DROP TYPE IF EXISTS ClearingType CASCADE;
CREATE TYPE ClearingType AS ENUM ('automatic', 'manual');

-- Table: versions, link alarm_dictionary and alarm_definitions using version.
DROP TABLE IF EXISTS versions CASCADE;
CREATE TABLE versions (
         version_number VARCHAR(50) PRIMARY KEY
);

-- Table: alarm_dictionary
DROP TABLE IF EXISTS alarm_dictionary CASCADE;
CREATE TABLE alarm_dictionary (
          resource_type_id UUID PRIMARY KEY,
          alarm_dictionary_version VARCHAR(50) NOT NULL,  -- Links alarm_dictionary and alarm_definitions
          alarm_dictionary_schema_version VARCHAR(50) DEFAULT 'TBD-O-RAN-DEFINED' NOT NULL,
          entity_type VARCHAR(255) NOT NULL,
          vendor VARCHAR(255) NOT NULL,
          management_interface_id ManagementInterfaceID[] DEFAULT ARRAY['O2IMS']::ManagementInterfaceID[],
          pk_notification_field TEXT[] DEFAULT ARRAY['resource_type_id']::TEXT[],
          created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
          CONSTRAINT fk_alarm_dictionary_version FOREIGN KEY (alarm_dictionary_version) REFERENCES versions(version_number)
);

-- Table: alarm_definitions
DROP TABLE IF EXISTS alarm_definitions CASCADE;
CREATE TABLE alarm_definitions (
           alarm_definition_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
           alarm_name VARCHAR(255) NOT NULL,
           alarm_last_change VARCHAR(50) NOT NULL,
           alarm_description TEXT NOT NULL,
           proposed_repair_actions TEXT NOT NULL,
           alarm_dictionary_version VARCHAR(50) NOT NULL, -- Links alarm_dictionary and alarm_definitions
           alarm_additional_fields JSONB,
           alarm_change_type AlarmLastChangeType DEFAULT 'added' NOT NULL,
           clearing_type ClearingType DEFAULT 'automatic' NOT NULL,
           management_interface_id ManagementInterfaceID[] DEFAULT ARRAY['O2IMS']::ManagementInterfaceID[],
           pk_notification_field TEXT[] DEFAULT ARRAY['alarm_definition_id']::TEXT[],
           created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
           CONSTRAINT fk_alarm_definitions_version FOREIGN KEY (alarm_dictionary_version) REFERENCES versions(version_number),
           CONSTRAINT unique_alarm_name_last_change UNIQUE (alarm_name, alarm_last_change)
);

-- Table: probable_causes
DROP TABLE IF EXISTS probable_causes CASCADE;
CREATE TABLE probable_causes (
             probable_cause_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
             alarm_definition_id UUID UNIQUE,
             FOREIGN KEY (alarm_definition_id) REFERENCES alarm_definitions(alarm_definition_id) ON DELETE CASCADE
);

-- Create a new entry for probable cause for each new alarm_definition_id
CREATE OR REPLACE FUNCTION insert_probable_cause()
    RETURNS TRIGGER AS $$
BEGIN
    -- Insert a new row into probable_causes with the alarm_definition_id from the new alarm_definition
    INSERT INTO probable_causes (alarm_definition_id)
    VALUES (NEW.alarm_definition_id);

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_insert_probable_cause
    AFTER INSERT ON alarm_definitions
    FOR EACH ROW
EXECUTE FUNCTION insert_probable_cause();


-- Table: alarm_subscription_info
DROP TABLE IF EXISTS alarm_subscription_info CASCADE;
CREATE TABLE alarm_subscription_info (
             subscription_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
             consumer_subscription_id UUID,
             filter TEXT,
             callback TEXT NOT NULL,
             event_cursor BIGINT NOT NULL DEFAULT 0,
             created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);


CREATE TYPE Status AS ENUM ('resolved', 'firing');
CREATE TYPE PerceivedSeverity AS ENUM ('CRITICAL', 'MAJOR', 'MINOR','WARNING', 'INDETERMINATE', 'CLEARED');

-- SEQUENCE: Counter to keep track of latest events and use it notify only the latest
CREATE SEQUENCE alarm_sequence_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Table: alarm_event_record
DROP TABLE IF EXISTS alarm_event_record CASCADE;
CREATE TABLE alarm_event_record (
            alarm_event_record_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            alarm_definition_id UUID NOT NULL,
            probable_cause_id UUID,
            status Status DEFAULT 'firing' NOT NULL,
            alarm_raised_time TIMESTAMPTZ NOT NULL,
            alarm_changed_time TIMESTAMPTZ,
            alarm_cleared_time TIMESTAMPTZ,
            alarm_acknowledged_time TIMESTAMPTZ,
            alarm_acknowledged BOOLEAN NOT NULL default FALSE,
            perceived_severity PerceivedSeverity NOT NULL,
            extensions JSONB,
            created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
            finger_print TEXT not null,
            alarm_sequence_number BIGINT DEFAULT nextval('alarm_sequence_seq'),
            resource_id UUID NOT NULL,
            resource_type_id UUID NOT NULL,
            CONSTRAINT fk_resource_type FOREIGN KEY (resource_type_id) REFERENCES alarm_dictionary (resource_type_id),
            CONSTRAINT unique_finger_print_alarm_raised_time UNIQUE (finger_print, alarm_raised_time)
);

-- Ownership of alarm_sequence_seq
ALTER SEQUENCE alarm_sequence_seq OWNED BY alarm_event_record.alarm_sequence_number;


-- Update the sequence if resolved or alarm_changed_time changed 
CREATE OR REPLACE FUNCTION set_alarm_sequence_on_update()
    RETURNS TRIGGER AS $$
BEGIN
    IF (NEW.status = 'resolved' AND OLD.status IS DISTINCT FROM 'resolved')
        OR (NEW.alarm_changed_time IS DISTINCT FROM OLD.alarm_changed_time) THEN
        NEW.alarm_sequence_number := nextval('alarm_sequence_seq');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- Attach the trigger to alarm_event_record
CREATE TRIGGER trg_alarm_sequence_update
    BEFORE UPDATE ON alarm_event_record
    FOR EACH ROW
EXECUTE FUNCTION set_alarm_sequence_on_update();


-- Table: alarm_event_record_archive, is identical to alarm_event_record. Use to this to eventually store events that are considered historical (i.e status: resolved)
DROP TABLE IF EXISTS alarm_event_record_archive CASCADE;
CREATE TABLE alarm_event_record_archive
(LIKE alarm_event_record INCLUDING ALL);
```

Please note that script above was used to test and need some updates for production e.g

- use `CREATE TABLE IF NOT EXISTS` and remove `DROP TABLE IF EXISTS`.
- do not use * when query
- do no use enum and instead go with a lookup table

  ```sql
  CREATE TABLE management_interfaces (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL
  );

    INSERT INTO management_interfaces (name) VALUES ('O1'), ('O2DMS'), ('O2IMS'), ('OpenFH');
    CREATE TABLE alarms_table (
    ...
    management_interface_id INT REFERENCES management_interfaces(id) NOT NULL DEFAULT 3
    );
  ```

Apply this during deployment as specified [here](#k8s-resources) via a migration tool as specified [here](#tooling-and-general-dev-guidelines).

## Init

Notes:

- Interacting with the following: `versions`, `alarm_dictionary` and `alarm_definitions`
- `alarm_dictionary` has a primary key `resource_type_id`, ensuring that each ResourceType has a unique AlarmDictionary.
- There's a one-to-many relation between `alarm_dictionary` and `alarm_definitions` via major.minor OCP version. The referential integrity and normalization version is done through `versions` table

```sql
INSERT INTO versions (version_number)
VALUES
    ('4.16'),
    ('4.17');

-- Most of the data in this table will come client-go calls
INSERT INTO alarm_dictionary (alarm_dictionary_version, resource_type_id, entity_type, vendor)
VALUES
    ('4.16', 'b3e7149e-d471-4d0f-aaa6-d5e9aa9e713a', 'telco-model-OpenShift-4.16.2', 'Red Hat'),
    ('4.16', '481688c8-2782-4534-a9de-88ca5154411d', 'telco-model-OpenShift-4.16.8', 'Red Hat' ),
    ('4.17', 'f8b9e100-fd9f-4923-b96f-89418e9c2560', 'telco-model-OpenShift-4.17.2', 'Red Hat');

-- Insert into alarm_definitions
INSERT INTO alarm_definitions (alarm_name, alarm_last_change, alarm_description, proposed_repair_actions , alarm_additional_fields)
VALUES
    ('NodeClockNotSynchronising','4.16', 'Clock not synchronising.','https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/NodeClockNotSynchronising.md','{"customKey": "customValue"}'),
    ('NodeClockNotSynchronising','4.17','Clock not synchronising.','https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/NodeClockNotSynchronising.md', '{"customKey": "customValue"}');
-- The data here mapped from PrometheusRule. See ##PrometheusRule to AlarmDefinition mapping section for more.

-- probable_cause will be auto populated. see the section "##Get Probable cause ID, name and description" for more details
```

## Detailed Server Instantiation

For a given OCP release, the alarmDefinitions and probableCauses are fixed, so these can be built up front. For CaaS alarms only one resource type, “NodeCluster”, all alarms map to it.

1. Query all managed clusters to get list of unique major.minor versions
   - Need to monitor for major.minor new versions
   - Audit on restarts

2. For each major.minor version query PromRules from one cluster of that version

3. Take the rules and build the corresponding alarmDefinition dictionary
   - Link alarm dictionary to the resourceType
   - alarmDictionary version will be that major.minor of cluster

4. Assuming DB table already created, persist each alarmDictionary as a DB table

5. Take all the PromRules and build the AlarmDefinition. See [here](#prometheusrule-to-alarmdefinition-mapping) for mapping.
   - A corresponding probablyCause row will be autometically added.

6. Apply the required CR to activate the internal endpoint for alertmanager notification. See [here](#steps-for-internalv1caas-alertsalertmanager) for more.

7. The server should now be ready to take requests.

### PrometheusRule to AlarmDefinition mapping

```yaml
    # Partial PrometheusRule to show NodeClockNotSynchronising
    apiVersion: monitoring.coreos.com/v1
    kind: PrometheusRule
    metadata:
      name: node-exporter-rules
      namespace: openshift-monitoring
    spec:
      groups:
        - name: node-exporter
          rules:
            - alert: NodeClockNotSynchronising  #### (alarm_definitions.alarm_name)
              annotations:
                description: Clock at {{ $labels.instance }} is not synchronising. Ensure NTP is configured on this host.
                runbook_url: https://github.com/openshift/runbooks/blob/master/alerts/cluster-monitoring-operator/NodeClockNotSynchronising.md #### (alarm_definitions.proposed_repair_actions)
                summary: Clock not synchronising. #### (alarm_definitions.alarm_description)
              expr: |
                min_over_time(node_timex_sync_status{job="node-exporter"}[5m]) == 0
                and
                node_timex_maxerror_seconds{job="node-exporter"} >= 16
              for: 10m
              labels: 
                severity: critical #### (alarm_definitions.alarm_additional_fields)
                customKey: customValue
 ```

Notes on Init phase

- `versions` table reflects unique `major-minor` version `ManagedClusters` currently deployed.
  To get available `major-minor` managed cluster, we can list `ManagedCluster` CR and look for label `openshiftVersion-major-minor`.

  ```shell
    oc get managedclusters
    ```
  
    ```yaml
    apiVersion: cluster.open-cluster-management.io/v1
    kind: ManagedCluster
    metadata:
      labels:
        openshiftVersion-major-minor: "4.16"
    ```

- `alarm_dictionary` essentially links `ResourceTypeID` and `Version`. The conversion can be seen below.

    | managed_cluster `from alerts         | resourceID  (same as managed_cluster) | resourceTypeID for caas (derived from (resourceID + ResourceKindLogical + ResourceClassCompute) |
    |--------------------------------------|---------------------------------------|-------------------------------------------------------------------------------------------------|
    | f90561e2-6420-4924-b081-f4f8eaf50618 | f90561e2-6420-4924-b081-f4f8eaf50618  | 4586f964-6c6f-407b-9b18-cb3c9a712ec4                                                            |

    `entity_type` data should be coming from Inventory API but for now we can hard-code it. `telco-model-OpenShift-<Full Version>`
- `alarm_definitions` reflects Rules in PromRule CR. We only grab the full set based on unique entries in `Versions` table
  - Use ACM to get credentials of the unique major.minor clusters and retrieve all the PrometheusRules from them to parse.
    E.g if we are managing 3 clusters 4.16.2, 4.17.2 and 4.16.8, Pick 4.16.8 and 4.17.2 which effectively represents all the rules in 4.16.z and 4.17.z clusters.
- Build out a mapping between cluster ID, resource type ID and resource ID in memory as needed for quick lookup during runtime.

## Ready

### For a given ResourceTypeID and AlarmName (coming from AM alert), find the AlarmDefinitionID and ProbableCauseID

- Find resourceType ID and alert name from current Alertmanager payload
  - Get managedcluster-id and alert name from alertmanager alerts (e.g NodeClockNotSynchronising2)
  - Ask inventory for ResourceType ID using managedcluster-id (e.g b3e7149e-d471-4d0f-aaa6-d5e9aa9e713a)
  
    ```go
    // should be something we compute 
    func getResourceTypeID(managedClusterId uuid.UUID, class ResourceClass, resourceType ResourceType) uuid.UUID {
      return v5UUID(fmt.Sprintf("%s-%s-%s", managedClusterId.String(), class, resourceType))
    }
    ```

- Run following. `resource_type_id` should be enough to uniquely identify the corresponding alarm dictionary
  and its associated alarm definitions and finally use `alarm_name` to filter out the exact rule.

  ```sql
    SELECT
        ad.alarm_definition_id,
        pc.probable_cause_id
    FROM
        alarm_definitions ad
        JOIN
          alarm_dictionary adict
          ON ad.alarm_dictionary_version = adict.alarm_dictionary_version
        JOIN
          probable_causes pc
          ON ad.alarm_definition_id = pc.alarm_definition_id
    WHERE
        adict.resource_type_id = '481688c8-2782-4534-a9de-88ca5154411d'
        AND ad.alarm_name = 'LowMemory';
    ```

### Notification tracking

- Collect all subscription info including ID, callback and filter
- For each subscription, collect all the `AlarmEventRecord` rows based on sequence number and optionally the filter. Here we are collecting everything that's "CRITICAL"

  ```sql
    SELECT aer.*
    FROM alarm_event_record aer
             JOIN alarm_subscription_info asi ON asi.alarm_subscription_id = 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'
    WHERE aer.alarm_sequence_number > asi.event_cursor
      and aer.perceived_severity = 'CRITICAL'
    ORDER BY aer.alarm_sequence_number;
    ```

- Process and notify by deriving `AlarmEventRecordModifications` O-RAN DS + callback.
- Update sequence for subscription indicating the latest event sent so far

    ```go
    var largestProcessedSequenceNumber int64
    // for each alarms Update the largest sequence number we've processed
    largestProcessedSequenceNumber = max(alarm.SequenceNumber, largestProcessedSequenceNumber))
    // The last row in the query is always the largest (ORDER BY default is accending)
    ```
  
  And finally update

  ```sql
  UPDATE alarm_subscription_info
  SET event_cursor = $largestProcessedSequenceNumber
  WHERE alarm_subscription_id = 'a2eebc99-9c0b-4ef8-bb6d-6bb9bd380a13';
  ```
  
- NOTE: `alarm_sequence_number` is automatically handled from inside the DB. When the sequence increments, subscriber is notified.
  See notification conditions [here](#conditions-for-notifying-subscriber)
  
### Conditions for Notifying subscriber

Details under 3.7.2 Alarm Notification Use Case in O-RAN-WG6.ORCH-USE-CASES-R003-v10.00 June 2024 (download from [here](https://specifications.o-ran.org/download?id=672))

- When alarm is `firing` for the first time
- When alarm is updated to `resolved`
- When `alarm_changed_time` changes (could be multiple times)

### Daily archive cleanup

Run this using a k8s CronJob CR at the start of every hour

```sql
DELETE FROM alarm_event_record_archive
WHERE alarm_cleared_time < NOW() - INTERVAL '24 hour' and status = 'resolved';
```

We can apply the CR before server starts and remove it during shutdown as part of teardown e.g inside `server.RegisterOnShutdown`

### Get Probable cause ID, name and description

As soon as a new AlarmDefinition row is added, we generate a new UUID and add a row to probable_cause table automatically (leveraging postgres trigger function) .
When a user queries for all or a specific probableCause, we will simply join the tables to get the data.

```sql
SELECT
    probable_causes.probable_cause_id,
    ad.alarm_name,
    ad.alarm_description
FROM probable_causes
         JOIN alarm_definitions ad
              on ad.alarm_definition_id = probable_causes.alarm_definition_id
WHERE probable_causes.probable_cause_id = 'f5ac4ac7-0ff3-40a2-b305-77313c28136a';
```

## K8s resources

We will need few K8s resources that will be eventually applied by the Operator.

### Jobs to Initialize Alarm Server DB

We need two Jobs that can help with DB

- One job that creates a Database using `CREATE DATBASE` cmd
  - ALARMS_DB: `alarms`
  - ALARMS_USER: `alarms`
  - ALARM_PASSWORD: `alarms`
- One job that creates all the tables as part of migration (create new tables, updates, etc)
  See [postgres](#postgres) for all the required credentials

#### Alarm server

This is essentially a typical CRUD app and we need the following

- Deployment: one main container which starts the main server.
  No HA, so set replica to 1. It should also contain all the ENV variables needed to talk to postgres deployment ALARMS_DB, POSTGRES_PORT, ALARMS_USER etc (consult with POSTGRES deployment + Init JOB for alarms for latest).
  DB_NAME will be set to whatever is used in [here](#jobs-to-initialize-alarm-server-db).
  Suitable resources should be provided but not much memory and CPU is need for CRUD apps (TBD, need to experiment for exact values).
- Secrets: DB creds and configs should be read from postgres deployment.
- Service: Expose and balance using `ClusterIP` (though to start with we will set replica to 1)
- Ingress: Expose service so that it can be called by users from outside the cluster

#### Postgres

This deployment can be leveraged by many microservices by creating their own Database.

- Deployment: One replica should be good enough for our case.
  Default username, password and dbname (note this is simply to allow postgres pod initialize) should be provided.
  Deployment should also mount to the right path `/var/lib/postgresql/data` (please check the latest docs).
- PersistentVolumeClaim: At least 20G PVC should be used. Using test, we used about 60k rows DB which took about ~2Gb
- Service: ClusterIP should be good as it's only used within the cluster.
- Secrets: default creds needed to spin postgres
  - POSTGRES_ADMIN_USER: `o-ran`
  - POSTGRES_ADMIN_PASSWORD: `o-ran`
- ConfigMap:
  - POSTGRES_HOST: "postgres.o-ran-namespace.svc.cluster.local"
  - POSTGRES_PORT: "5432"
  - POSTGRES_DEFAULT_DB: `o-ran` # Note: this is simply there to be explicit. If not provided `POSTGRES_USER` is used to create default DB. But ultimately this DB will not be used as each service will create their own.

With the Secrets and ConfigMap applied, an app may connect to the DB with the following given the service (assuming initContainer of app already created DB `alarms`):

```go
connStr := "postgres://alarms:alarms@postgres.o-ran-namespace.svc.cluster.local:5432/alarms?sslmode=disable"
```

Note:

- ODF (ceph operator) must be deployed on the hub cluster that database requests a pvc from (same storageclass).
  This will allow for replicated persistent storage

## Tooling and general dev guidelines

- All the features stated above will be developed within in the same server.
  It will expose all the external apis (o-ran) and internal apis (alertmanager + future h/w) and
  handle them concurrently to drive all the features.
- The HTTP server should be built with latest Go 1.22 `net/http` std lib. The latest update in the package brings in
  many requested features including mapping URI pattern. This allows us to not rely on third party lib such as `gorilla/mux`.
- Prefer creating structs to hold HTTP data for idiomatic Go code.
- OpenAPI spec should be the source of truth. Other than standardization, free validation and documentation,
  with it, we can leverage a code generator such [this](https://github.com/oapi-codegen/oapi-codegen), allowing us to avoid writing boilerplate code.
- For Postgres communication use library [pgx](https://github.com/jackc/pgx) v5. This Go Postgres driver and lots of
  important features such as automatic type mapping, detailed error reporting (capture performance info) etc.
  There's also many ORM and SQL query builder libraries but pgx looks like the best of both worlds.
- DB migration is generally handled with a different tool. [golang-migrate](https://github.com/golang-migrate/migrate) is generally used for this which we can call during service init.
- Introduce server specific commands. In this case we need something that can init a new DB and migrate as needed E.g

  ```shell
  oran-o2ims alarms db-migration -h
  ```
  
## Future Updates

Update `sslmode` to true as needed to securely communicate with pods. E.g postgres and alertmanager  

Phase 2: CaaS + H/W Alarms

- Add H/W alarms with phase 1 capabilities

Phase 3: MNO Support

- Phase 1 + 2 capabilities
- Scale target: 2 MNO clusters with 7 nodes

Phase 4: GA

- Scale to 3500 SNO clusters per hub
- Scale to `TBD` MNO clusters per hub
- Increased number of subscriptions `TBD`
- Support alarm suppression
- Support configurable historical alarm retention period
- HA `TBD`
