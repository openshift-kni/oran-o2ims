-- Holds information about the alarm service configuration
CREATE TABLE IF NOT EXISTS alarm_service_configuration (
    -- O-RAN
    retention_period INT NOT NULL, -- Number of days for alarm history to be retained.
    extensions JSONB, -- Additional data for extensibility

    -- Internal
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique identifier for each alarm service configuration
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record creation timestamp
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP -- Record last update timestamp
);

-- Trigger function to update updated_at timestamp for alarm_service_configuration
CREATE OR REPLACE FUNCTION update_alarm_service_configuration_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to execute update_alarm_service_configuration_timestamp before each update on alarm_service_configuration
CREATE OR REPLACE TRIGGER update_alarm_service_configuration_timestamp
    BEFORE UPDATE ON alarm_service_configuration
    FOR EACH ROW
    EXECUTE FUNCTION update_alarm_service_configuration_timestamp();
