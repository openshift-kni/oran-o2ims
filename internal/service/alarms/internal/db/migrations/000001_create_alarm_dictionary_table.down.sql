-- Drop the trigger that updates updated_at for alarm_dictionary
DROP TRIGGER IF EXISTS set_alarm_dictionary_timestamp ON alarm_dictionary;

-- Drop the function used by the alarm_dictionary trigger
DROP FUNCTION IF EXISTS update_alarm_dictionary_timestamp;

-- Drop the alarm_dictionary table
DROP TABLE IF EXISTS alarm_dictionary;
