-- Remove the default for alarm_sequence_number that uses alarm_sequence_seq
ALTER TABLE alarm_event_record ALTER COLUMN alarm_sequence_number DROP DEFAULT;

-- Drop the trigger for updating alarm_sequence_number in alarm_event_record
DROP TRIGGER IF EXISTS update_alarm_event_sequence ON alarm_event_record;

-- Drop the trigger function for updating alarm_sequence_number
DROP FUNCTION IF EXISTS update_alarm_event_sequence;

-- Drop the sequence for alarm_sequence_number
DROP SEQUENCE IF EXISTS alarm_sequence_seq;

-- Drop the alarm_event_record table
DROP TABLE IF EXISTS alarm_event_record;
