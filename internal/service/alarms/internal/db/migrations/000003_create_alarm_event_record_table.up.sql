-- Counter to keep track of the latest events, used to notify only the latest event.
CREATE SEQUENCE IF NOT EXISTS alarm_sequence_seq
    START WITH 1
    INCREMENT BY 1
    MINVALUE 1
    NO MAXVALUE
    CACHE 1;

-- Logs individual alarm events. alarm_event_record also contains all the data needed for generating AlarmEventNotification
CREATE TABLE IF NOT EXISTS alarm_event_record (
    -- O-RAN
    alarm_event_record_id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique identifier for each event record
    alarm_definition_id UUID NOT NULL, -- From alarm_definition table
    probable_cause_id UUID NOT NULL, -- From alarm_definition table
    alarm_raised_time TIMESTAMPTZ NOT NULL, -- From current alert notification
    alarm_changed_time TIMESTAMPTZ, -- From current alert notification
    alarm_cleared_time TIMESTAMPTZ, -- From current alert notification
    alarm_acknowledged_time TIMESTAMPTZ, -- From current alert notification
    alarm_acknowledged BOOLEAN NOT NULL DEFAULT FALSE, -- From PATCH api request but default to false
    perceived_severity INT NOT NULL, -- We will need to map the current alert with this from code
    extensions JSONB, -- Additional data for extensibility

    -- O-RAN additional data to create AlarmEventNotification
    resource_id UUID NOT NULL, -- Same as manager_cluster_id for caas alerts
    resource_type_id UUID NOT NULL, -- Derived from manager_cluster_id
    notification_event_type INT NOT NULL, -- Should be enum calculated from current alert

    -- Internal
    alarm_status VARCHAR(20) DEFAULT 'firing' NOT NULL, -- Status of the alarm (either 'firing' or 'resolved'). This is also used to archive it later.
    fingerprint TEXT NOT NULL, -- Unique identifier of caas alerts
    alarm_sequence_number BIGINT DEFAULT nextval('alarm_sequence_seq'), -- Sequential number for ordering events. This is used to notify subsriber
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record creation timestamp

    CONSTRAINT unique_fingerprint_alarm_raised_time UNIQUE (fingerprint, alarm_raised_time), -- Unique constraint to prevent duplicate caas alert with the same fingerprint and time
    CONSTRAINT chk_status CHECK (alarm_status IN ('firing', 'resolved')), -- Check constraint to enforce status as either 'firing' or 'resolved'
    CONSTRAINT chk_perceived_severity CHECK (perceived_severity IN (0, 1, 2, 3, 4, 5))  -- Check constraint to restrict perceived_severity to valid integer values. See generated ENUMs in server for more.
);

-- Set ownership of the alarm_sequence_seq sequence to alarm_event_record.alarm_sequence_number
ALTER SEQUENCE alarm_sequence_seq OWNED BY alarm_event_record.alarm_sequence_number;

-- Function to update the alarm_sequence_number on specific status or time changes
CREATE OR REPLACE FUNCTION update_alarm_event_sequence()
    RETURNS TRIGGER AS $$
BEGIN
    -- Update sequence if status changes to 'resolved' or if alarm_changed_time is updated
    IF (NEW.status = 'resolved' AND OLD.status IS DISTINCT FROM 'resolved')
       OR (NEW.alarm_changed_time IS DISTINCT FROM OLD.alarm_changed_time) THEN
        NEW.alarm_sequence_number := nextval('alarm_sequence_seq');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to execute update_alarm_event_sequence before updating alarm_event_record
CREATE TRIGGER update_alarm_event_sequence
    BEFORE UPDATE ON alarm_event_record
    FOR EACH ROW
    EXECUTE FUNCTION update_alarm_event_sequence();
