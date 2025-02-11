-- Drop the trigger for managing events
DROP TRIGGER IF EXISTS manage_alarm_event ON alarm_event_record;

-- Drop the function for managing events
DROP FUNCTION IF EXISTS manage_alarm_event;

-- Drop the alarm_event_record table
DROP TABLE IF EXISTS alarm_event_record;

-- Drop the data_change_event table
DROP TABLE IF EXISTS data_change_event;

DROP TRIGGER IF EXISTS alarm_event_after_trigger ON alarm_event_record;
DROP FUNCTION IF EXISTS manage_alarm_event_after();
