-- Holds information about each unique resource type and its associated dictionary details
CREATE TABLE IF NOT EXISTS alarm_dictionary (
    -- O-RAN
    alarm_dictionary_version VARCHAR(50) NOT NULL, -- Version of the alarm dictionary, potentially in major.minor format
    alarm_dictionary_schema_version VARCHAR(50) DEFAULT 'TBD-O-RAN-DEFINED' NOT NULL, -- Schema version, defaulted to TBD-O-RAN-DEFINED
    entity_type VARCHAR(255) NOT NULL, -- Combination of ResourceType.model and ResourceType.version
    vendor VARCHAR(255) NOT NULL, -- ResourceType.vendor field
    management_interface_id VARCHAR(50)[] DEFAULT ARRAY['O2IMS']::VARCHAR[], -- Management interfaces, defaults to o2ims
    pk_notification_field TEXT[] DEFAULT ARRAY['alarm_dictionary_id']::TEXT[], -- Primary key notification field, defaults to alarm_dictionary_id

    -- Internal
    alarm_dictionary_id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique identifier for each alarm dictionary
    resource_type_id UUID NOT NULL, -- One-to-one relation between a resourceType and alarmDictionary
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record creation timestamp
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP -- Record last update timestamp
);

-- Trigger function to update updated_at timestamp for alarm_dictionary
CREATE OR REPLACE FUNCTION update_alarm_dictionary_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to execute update_alarm_dictionary_timestamp before each update on alarm_dictionary
CREATE OR REPLACE TRIGGER set_alarm_dictionary_timestamp
    BEFORE UPDATE ON alarm_dictionary
    FOR EACH ROW
    EXECUTE FUNCTION update_alarm_dictionary_timestamp();
