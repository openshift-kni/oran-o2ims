-- Stores information about alarm subscriptions
CREATE TABLE IF NOT EXISTS alarm_subscription_info (
    -- O-RAN
    subscription_id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique identifier for each subscription
    consumer_subscription_id UUID, -- Optional ID for the consumer's subscription identifier
    filter TEXT, -- Criteria for filtering alarms, if any
    callback TEXT NOT NULL, -- URL or endpoint for sending notifications

    -- Internal
    event_cursor BIGINT NOT NULL DEFAULT 0, -- Tracks the latest event for the subscriber. This used with alarm_event_record.alarm_sequence_number
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record creation timestamp
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP -- Record last update timestamp
);

-- Function to update the updated_at column on modification
CREATE OR REPLACE FUNCTION update_subscription_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger execute update_subscription_timestamp to update the updated_at timestamp for alarm_subscription_info
CREATE TRIGGER update_subscription_timestamp
    BEFORE UPDATE ON alarm_subscription_info
    FOR EACH ROW
    EXECUTE FUNCTION update_subscription_timestamp();
