-- Drop the trigger that updates updated_at for alarm_service_configuration
DROP TRIGGER IF EXISTS update_alarm_service_configuration_timestamp ON alarm_service_configuration;

-- Drop the function used by the alarm_service_configuration trigger
DROP FUNCTION IF EXISTS update_alarm_service_configuration_timestamp;

-- Drop the alarm_service_configuration table
DROP TABLE IF EXISTS alarm_service_configuration;
