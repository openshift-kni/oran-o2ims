-- Create the archive table, mirroring the structure of alarm_event_record. Store historical alarms here and then remove them as needed.
-- Intentionally no constraints or trigger to update value as this table simply archive
CREATE TABLE IF NOT EXISTS alarm_event_record_archive (
    alarm_event_record_id UUID PRIMARY KEY,
    alarm_definition_id UUID ,
    probable_cause_id UUID ,
    alarm_raised_time TIMESTAMPTZ ,
    alarm_changed_time TIMESTAMPTZ,
    alarm_cleared_time TIMESTAMPTZ,
    alarm_acknowledged_time TIMESTAMPTZ,
    alarm_acknowledged BOOLEAN DEFAULT FALSE,
    perceived_severity INT,
    extensions JSONB,
    resource_id UUID,
    resource_type_id UUID,
    alarm_status VARCHAR(20) DEFAULT 'firing',
    fingerprint TEXT,
    alarm_sequence_number BIGINT, -- Static; no auto-increment
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
