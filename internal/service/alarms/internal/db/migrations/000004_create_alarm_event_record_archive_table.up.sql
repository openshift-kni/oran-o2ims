-- Create the archive table, mirroring the structure of alarm_event_record. Store historical alarms here and then remove them as needed.
-- Intentionally no constrains or trigger to update value as this table simply archive
CREATE TABLE IF NOT EXISTS alarm_event_record_archive (
    alarm_event_record_id UUID PRIMARY KEY,
    alarm_definition_id UUID NOT NULL,
    probable_cause_id UUID NOT NULL,
    alarm_raised_time TIMESTAMPTZ NOT NULL,
    alarm_changed_time TIMESTAMPTZ,
    alarm_cleared_time TIMESTAMPTZ,
    alarm_acknowledged_time TIMESTAMPTZ,
    alarm_acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
    perceived_severity INT NOT NULL,
    extensions JSONB,
    resource_id UUID NOT NULL,
    resource_type_id UUID NOT NULL,
    notification_event_type INT NOT NULL,
    alarm_status VARCHAR(20) DEFAULT 'firing' NOT NULL,
    finger_print TEXT NOT NULL,
    alarm_sequence_number BIGINT, -- Static; no auto-increment
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
