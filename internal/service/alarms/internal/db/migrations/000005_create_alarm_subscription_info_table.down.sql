-- Drop the trigger for updating the updated_at timestamp
DROP TRIGGER IF EXISTS update_subscription_timestamp ON alarm_subscription_info;

-- Drop the function that updates the updated_at timestamp
DROP FUNCTION IF EXISTS update_subscription_timestamp;

-- Drop the alarm_subscription_info table
DROP TABLE IF EXISTS alarm_subscription_info;
