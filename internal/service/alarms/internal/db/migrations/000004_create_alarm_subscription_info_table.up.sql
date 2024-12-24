-- Stores information about alarm subscriptions
-- Ref: O-RAN.WG6.O-CLOUD-IM.0-R004-v02.00
CREATE TABLE IF NOT EXISTS alarm_subscription_info (
    -- O-RAN
    subscription_id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique identifier for each subscription
    -- consumerSubscriptionID does not explicitly set UUID in format (likely someone forgot to add it), however, inventory subscription does, so we will use UUID
    consumer_subscription_id UUID NULL, -- Optional ID for the consumer's subscription identifier
    -- filter set nullable as false, but description says that if not set, all alarms are included
    -- setting as NULL based on description and model definition in O-RAN.WG6.O2IMS-INTERFACE-R004-v07.00
    filter VARCHAR(20) NULL, -- Can be [new, change, clear, acknowledge], NULL means all
    callback TEXT NOT NULL, -- URL or endpoint for sending notifications

    -- Internal
    event_cursor BIGINT NOT NULL DEFAULT 0, -- Tracks the latest event for the subscriber. This used with alarm_event_record.alarm_sequence_number
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP, -- Record creation timestamp
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP -- Record last update timestamp

    -- check does not fail with NULL
    CONSTRAINT chk_filter CHECK (filter IN ('new', 'change', 'clear', 'acknowledge')),
    CONSTRAINT unique_callback UNIQUE (callback)
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
CREATE OR REPLACE TRIGGER update_subscription_timestamp
    BEFORE UPDATE ON alarm_subscription_info
    FOR EACH ROW
    EXECUTE FUNCTION update_subscription_timestamp();
