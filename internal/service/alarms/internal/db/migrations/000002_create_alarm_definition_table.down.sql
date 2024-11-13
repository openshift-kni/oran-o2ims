-- Drop the trigger for setting resource_type_id in alarm_definition
DROP TRIGGER IF EXISTS populate_alarm_definition_resource_type_id ON alarm_definition;

-- Drop the trigger function for setting resource_type_id in alarm_definition
DROP FUNCTION IF EXISTS set_alarm_definition_resource_type_id;

-- Drop the trigger for updating updated_at in alarm_definition
DROP TRIGGER IF EXISTS set_alarm_definition_updated_at ON alarm_definition;

-- Drop the trigger function for updating updated_at in alarm_definition
DROP FUNCTION IF EXISTS update_alarm_definition_timestamp;

-- Drop the alarm_definition table
DROP TABLE IF EXISTS alarm_definition;
