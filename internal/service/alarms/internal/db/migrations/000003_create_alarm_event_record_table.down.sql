-- Drop the trigger for managing events
DROP TRIGGER IF EXISTS manage_alarm_event ON alarm_event_record;

-- Drop the function for managing events
DROP FUNCTION IF EXISTS manage_alarm_event;

-- Remove the default for alarm_sequence_number that uses alarm_sequence_seq
ALTER TABLE alarm_event_record ALTER COLUMN alarm_sequence_number DROP DEFAULT;

-- Drop the sequence for alarm_sequence_number
DROP SEQUENCE IF EXISTS alarm_sequence_seq;

-- Drop the alarm_event_record table
DROP TABLE IF EXISTS alarm_event_record;
