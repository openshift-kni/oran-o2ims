-- Outbox pattern table
-- All changes in alarms event lifecycle that needs a corresponding notification is added to this table
CREATE TABLE IF NOT EXISTS data_change_event (
    data_change_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_type    VARCHAR(64)  NOT NULL, -- Table name reference
    object_id      UUID         NOT NUll, -- Primary key
    parent_id      UUID         NULL,
    before_state   json         NULL,
    after_state    json         NULL,
    sequence_id    SERIAL,               -- track insertion order rather than rely on timestamp since precision may cause ambiguity
    created_at     TIMESTAMPTZ  DEFAULT CURRENT_TIMESTAMP
);

-- Works with data_change_event outbox to track the high watermark
CREATE TABLE IF NOT EXISTS notification_cursor (
  id SERIAL PRIMARY KEY,
  last_event_id BIGINT NOT NULL
);

-- Insert an initial row with last_event_id = 0 if the table is empty.
INSERT INTO notification_cursor (last_event_id)
SELECT 0
WHERE NOT EXISTS (SELECT 1 FROM notification_cursor);
