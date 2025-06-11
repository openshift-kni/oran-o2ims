-- Logs individual alarm events. alarm_event_record also contains all the data needed for generating AlarmEventNotification
CREATE TABLE IF NOT EXISTS alarm_event_record (
    -- O-RAN
    alarm_event_record_id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique identifier for each event record
    alarm_definition_id UUID, -- From alarm_definition table. Note: nullable to capture if ACM is sending us cluster ID
    probable_cause_id UUID, -- From alarm_definition table. Note: nullable to capture if ACM is sending us cluster ID
    alarm_raised_time TIMESTAMPTZ NOT NULL, -- From current alert notification
    alarm_changed_time TIMESTAMPTZ, -- From current alert notification
    alarm_cleared_time TIMESTAMPTZ, -- From current alert notification
    alarm_acknowledged_time TIMESTAMPTZ, -- From current alert notification
    alarm_acknowledged BOOLEAN NOT NULL DEFAULT FALSE, -- From PATCH api request but default to false
    perceived_severity INT NOT NULL, -- We will need to map the current alert with this from code
    extensions JSONB, -- Additional data for extensibility

    -- O-RAN additional data to create AlarmEventNotification
    object_id UUID, -- Same as manager_cluster_id for caas alerts. Note: nullable to capture if ACM is sending us cluster ID
    object_type_id UUID, -- Derived from manager_cluster_id. Note: nullable to capture if ACM is sending us cluster ID
    notification_event_type VARCHAR(20) DEFAULT 'NEW' NOT NULL, -- Same as alarm_subscription_info.filter used to quickly filter and return notification

    -- Internal
    alarm_status VARCHAR(20) DEFAULT 'firing' NOT NULL, -- Status of the alarm (either 'firing' or 'resolved'). This is also used to archive it later.
    fingerprint TEXT NOT NULL, -- Unique identifier of caas alerts
    should_create_data_change_event BOOLEAN DEFAULT TRUE NOT NULL, -- This flag is transient and is re-evaluated with each change to the alarm event row which potentially results a new entry in outbox using AFTER trigger.
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record creation timestamp
    generation_id BIGINT DEFAULT 0 NOT NULL, -- GenerationID to determine if an event is stale
    alarm_source TEXT DEFAULT 'alertmanager' NOT NULL, -- Source of event such as AM, hardware etc

    CONSTRAINT unique_fingerprint_alarm_raised_time UNIQUE (fingerprint, alarm_raised_time), -- Unique constraint to prevent duplicate caas alert with the same fingerprint and time
    CONSTRAINT chk_status CHECK (alarm_status IN ('firing', 'resolved')), -- Check constraint to enforce status as either 'firing' or 'resolved'
    CONSTRAINT chk_perceived_severity CHECK (perceived_severity IN (0, 1, 2, 3, 4, 5)),  -- Check constraint to restrict perceived_severity to valid integer values. See generated ENUMs in server for more.
    CONSTRAINT chk_notification_event_type CHECK (notification_event_type IN ('NEW', 'CHANGE', 'CLEAR', 'ACKNOWLEDGE')) -- Validate notification_event_type (same as alarm_subscription_info.filter)
);


/*
Manages alarm lifecycle of alarm events.

The trigger manage_alarm_event is called BEFORE INSERT OR UPDATE to manage:
- alarm_changed_time: Tracks when alarm state or attributes last changed
- notification_event_type: Indicates type of change (CLEAR/ACKNOWLEDGE/CHANGE)
- should_create_data_change_event: determines if outbox entry can be created

For new alarms (INSERT):
- Sets alarm_changed_time to alarm_raised_time
- Sets CLEAR notification if initially resolved and alarm_changed_time to alarm_cleared_time
- should_create_data_change_event set to true

State transition priority (UPDATE):
1. Alarm State Change (CLEAR) - When status becomes 'resolved' (from 'new')
2. Alarm State Change (NEW) - When status becomes 'firing' (from 'resolved')
3. Acknowledgment (ACKNOWLEDGE) - On first acknowledgment
4. Attribute Changes (CHANGE) - For unacknowledged alarms only

should_create_data_change_event is set to true to indicate an outbox entry can be created for the following conditions:
- Brand new event
- Alarms status moves from resolved to firing
- Alarm status changes firing to resolved
- First acknowledgment
- Changes to key attributes (if not acknowledged)

alarm_changed_time updates:
- On change to resolved status from non-resolved, with alarm_cleared_time
- On changes to key attributes, with current time (if not acknowledged)
- On first time acknowledged, with alarm_acknowledged_time

AFTER INSERT OR UPDATE is called to insert to outbox if should_create_data_change_event is true.

*/

-- BEFORE trigger function: Compute state changes based on OLD and NEW values.
CREATE OR REPLACE FUNCTION manage_alarm_event()
RETURNS TRIGGER AS $$
BEGIN
    -- Handle new alarms
    IF TG_OP = 'INSERT' THEN
        NEW.alarm_changed_time := NEW.alarm_raised_time;
        NEW.should_create_data_change_event := TRUE;

        -- Set CLEAR and alarm_changed_time to alarm_cleared_time if alarm is initially resolved
        IF NEW.alarm_status = 'resolved' THEN
            NEW.alarm_changed_time = NEW.alarm_cleared_time;
            NEW.notification_event_type := 'CLEAR';
        END IF;

        RETURN NEW;

    -- Handle updates to existing alarms
    ELSIF TG_OP = 'UPDATE' THEN
        -- Reset should_create_data_change_event flag for any significant change
        NEW.should_create_data_change_event := FALSE;

        -- 1. Transition from firing to resolved.
        -- We consider a resolved state to be end of the event lifecycle,
        -- meaning even if something changes to event (e.g a new alarm definition) after the state is
        -- resolved we keep it "locked" and no more alerts sent this particular event.
        IF NEW.alarm_status = 'resolved' THEN
            IF NEW.alarm_status IS DISTINCT FROM OLD.alarm_status THEN
                NEW.notification_event_type := 'CLEAR';
                NEW.alarm_changed_time = NEW.alarm_cleared_time;
                NEW.should_create_data_change_event := TRUE;
            END IF;

        -- 2. Transition from resolved to firing. This is rare but this can happen.
        ELSIF OLD.alarm_status = 'resolved' AND NEW.alarm_status = 'firing' THEN
            NEW.notification_event_type := 'NEW';
            NEW.alarm_changed_time := CURRENT_TIMESTAMP;
            NEW.alarm_cleared_time := NULL;
            NEW.should_create_data_change_event := TRUE;

        -- 3. Handling alarm_acknowledged. Set alarm_changed_time to alarm_acknowledged_time
        ELSIF NEW.alarm_acknowledged THEN
            NEW.notification_event_type := 'ACKNOWLEDGE';
             -- Update sequence only on first acknowledgment
            IF NEW.alarm_acknowledged IS DISTINCT FROM OLD.alarm_acknowledged THEN
                NEW.alarm_changed_time = NEW.alarm_acknowledged_time;
                NEW.should_create_data_change_event := TRUE;
            END IF;

        -- 4. Other changes (only if not acknowledged)
        ELSIF NOT NEW.alarm_acknowledged THEN
            IF (NEW.object_id IS DISTINCT FROM OLD.object_id OR
                NEW.object_type_id IS DISTINCT FROM OLD.object_type_id OR
                NEW.alarm_definition_id IS DISTINCT FROM OLD.alarm_definition_id OR
                NEW.probable_cause_id IS DISTINCT FROM OLD.probable_cause_id)
            THEN
                NEW.notification_event_type := 'CHANGE';
                NEW.alarm_changed_time := CURRENT_TIMESTAMP;
                NEW.should_create_data_change_event := TRUE;
            END IF;
        END IF;

        RETURN NEW;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- Create a trigger for managing alarm events on INSERT or UPDATE
CREATE TRIGGER manage_alarm_event
    BEFORE INSERT OR UPDATE
    ON alarm_event_record
    FOR EACH ROW
    EXECUTE FUNCTION manage_alarm_event();

-- AFTER trigger function: Insert into outbox if should_create_data_change_event
CREATE OR REPLACE FUNCTION manage_alarm_event_after()
RETURNS TRIGGER AS $$
BEGIN
   IF NEW.should_create_data_change_event THEN
       INSERT INTO data_change_event (
           object_type,
           object_id,
           before_state,
           after_state
       )
       VALUES (
           'alarm_event_record',
           NEW.alarm_event_record_id,
           CASE WHEN TG_OP = 'UPDATE' THEN row_to_json(OLD) ELSE NULL END,
           row_to_json(NEW)
       );

       -- Multiple identical payloads in same transaction collapse to one notification
       -- So with one notification we are able to serially forward the call using high watermark in code
       PERFORM pg_notify('alarm_event_record_outbox_queued', json_build_object(
           'batch_update', true
       )::text);
   END IF;

   RETURN NEW;
END;
$$ LANGUAGE plpgsql;


CREATE TRIGGER alarm_event_after_trigger
AFTER INSERT OR UPDATE ON alarm_event_record
FOR EACH ROW
EXECUTE FUNCTION manage_alarm_event_after();
