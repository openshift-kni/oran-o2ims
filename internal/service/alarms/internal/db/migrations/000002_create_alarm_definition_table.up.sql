-- Defines specific alarms associated with a resource type, referencing alarm_dictionary
CREATE TABLE IF NOT EXISTS alarm_definition (
    -- O-RAN
    alarm_definition_id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique identifier for each alarm
    alarm_name VARCHAR(255) NOT NULL, -- Short name for the alarm
    alarm_last_change VARCHAR(50) NOT NULL, -- Version in which this alarm last changed. Can use alarmDict version
    alarm_change_type VARCHAR(20) DEFAULT 'added' NOT NULL, -- Type of change (added, deleted, modified)
    alarm_description TEXT NOT NULL, -- For caas it's rules[].summary
    proposed_repair_actions TEXT NOT NULL, -- For caas it's rules[].runbook_url
    clearing_type VARCHAR(20) DEFAULT 'automatic' NOT NULL, -- Clearing type (automatic or manual)
    management_interface_id VARCHAR(20)[] DEFAULT ARRAY['O2IMS']::VARCHAR[], -- Use default
    pk_notification_field TEXT[] DEFAULT ARRAY['alarmDefinitionID']::TEXT[], -- Use default
    alarm_additional_fields JSONB, -- In case of caas alerts, add anything that we didnt read from annotations or labels of the rules. Any data from PrometheusRule CR (where the rules are) may also dumped here.

    -- Internal
    alarm_dictionary_id UUID NOT NULL, -- Foreign key to alarm_dictionary to create a one-to-many relationship
    resource_type_id UUID NOT NULL, -- Duplicate for faster querying, and this will be auto added with trigger. During runtime (for caas) we are only guaranteed to get name and managed_cluster_id (resource_type_id is mapped to managed_cluster_id).
    probable_cause_id UUID DEFAULT gen_random_uuid(), -- Embedding this here to simplify schema. If we user asks for pc (directly or through event) we simply return the row with pc_id, alarm_name and alarm_description.
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record creation timestamp, Auto
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record last update timestamp, Auto

    CONSTRAINT unique_resource_type_alarm_name UNIQUE (resource_type_id, alarm_name), -- Ensures unique alarm names per resource type. Assume that this is always true, but adding a check in case case we ever miss something. This also optimizes lookup.
    CONSTRAINT fk_alarm_dictionary FOREIGN KEY (alarm_dictionary_id) REFERENCES alarm_dictionary(alarm_dictionary_id) ON DELETE CASCADE, -- Delete auto
    CONSTRAINT chk_alarm_change_type CHECK (alarm_change_type IN ('added', 'deleted', 'modified')),
    CONSTRAINT chk_clearing_type CHECK (clearing_type IN ('automatic', 'manual'))
);


-- Trigger function to set resource_type_id in alarm_definition
CREATE OR REPLACE FUNCTION set_alarm_definition_resource_type_id()
    RETURNS TRIGGER AS $$
BEGIN
    -- Set resource_type_id based on the associated alarm_dictionary_id
    NEW.resource_type_id := (SELECT resource_type_id FROM alarm_dictionary WHERE alarm_dictionary_id = NEW.alarm_dictionary_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to populate resource_type_id
CREATE OR REPLACE TRIGGER populate_alarm_definition_resource_type_id
    BEFORE INSERT OR UPDATE ON alarm_definition
    FOR EACH ROW
    EXECUTE FUNCTION set_alarm_definition_resource_type_id();

-- Trigger function to update updated_at on row updates for alarm_definition
CREATE OR REPLACE FUNCTION update_alarm_definition_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to execute update_alarm_definition_timestamp before each update on alarm_definition
CREATE OR REPLACE TRIGGER set_alarm_definition_updated_at
    BEFORE UPDATE ON alarm_definition
    FOR EACH ROW
    EXECUTE FUNCTION update_alarm_definition_timestamp();
