-- Table: data_source
CREATE TABLE IF NOT EXISTS data_source
(
    data_source_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(255) NOT NULL,
    generation_id INTEGER      NOT NULL DEFAULT 0,                 -- incremented on each audit/sync
    last_snapshot  TIMESTAMPTZ NULL,                               -- last completed snapshot
    created_at     TIMESTAMPTZ      DEFAULT CURRENT_TIMESTAMP,     -- TBD; tracks when first imported
    UNIQUE (name)
);

-- Table: resource_type
CREATE TABLE IF NOT EXISTS resource_type
(
    resource_type_id UUID PRIMARY KEY,
    name             VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    vendor           VARCHAR(64)  NOT NULL,
    model            VARCHAR(64)  NOT NULL,
    version          VARCHAR(64)  NOT NULL,
    resource_kind  VARCHAR(32) NOT NULL,                                 -- enum of physical, logical, etc...
    resource_class VARCHAR(32) NOT NULL,                                 -- enum of compute, networking, storage, etc...
    extensions       json         NULL,
    data_source_id UUID NOT NULL,
    generation_id    INTEGER      NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ           DEFAULT CURRENT_TIMESTAMP, -- TBD; tracks when first imported
    FOREIGN KEY (data_source_id) REFERENCES data_source (data_source_id) -- Manual cascade required for events
);

-- Table: resource_pool
CREATE TABLE IF NOT EXISTS resource_pool
(
    resource_pool_id   UUID PRIMARY KEY,
    global_location_id UUID NOT NULL,
    name               VARCHAR(255) NOT NULL,
    description        TEXT        NOT NULL,
    o_cloud_id         UUID         NOT NULL,
    location           VARCHAR(64) NULL,
    extensions         json         NULL,
    data_source_id     UUID        NOT NULL,
    generation_id      INTEGER      NOT NULL DEFAULT 0,
    external_id VARCHAR(255) NOT NULL,                                  -- FQDN of resource in downstream data source (e.g., id=XXX)
    created_at         TIMESTAMPTZ           DEFAULT CURRENT_TIMESTAMP, -- TBD; tracks when first imported
    FOREIGN KEY (data_source_id) REFERENCES data_source (data_source_id) -- Manual cascade required for events
);

-- Table: resource
CREATE TABLE IF NOT EXISTS resource
(
    resource_id      UUID PRIMARY KEY,
    description      TEXT          NOT NULL,
    resource_type_id UUID         NOT NULL,
    global_asset_id  VARCHAR(255) NULL,
    resource_pool_id UUID          NOT NULL,
    extensions       json         NULL,
    groups           VARCHAR(64)[] NULL,
    tags             VARCHAR(64)[] NULL,
    data_source_id UUID NOT NULL,
    generation_id    INTEGER      NOT NULL DEFAULT 0,
    external_id      VARCHAR(255),                                    -- FQDN of resource in downstream data source (e.g., id=XXX)
    created_at       TIMESTAMPTZ           DEFAULT CURRENT_TIMESTAMP, -- TBD; tracks when first imported
    FOREIGN KEY (data_source_id) REFERENCES data_source (data_source_id) -- Manual cascade required for events
);

-- Table: resource_pool_member
CREATE TABLE IF NOT EXISTS resource_pool_member
(
    resource_pool_id UUID NOT NULL,
    resource_id      UUID NOT NULL,
    PRIMARY KEY (resource_pool_id, resource_id),
    FOREIGN KEY (resource_pool_id) REFERENCES resource_pool (resource_pool_id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resource (resource_id) ON DELETE CASCADE
);

-- Table: deployment_manager
CREATE TABLE IF NOT EXISTS deployment_manager
(
    deployment_manager_id UUID PRIMARY KEY,
    name           VARCHAR(255)  NOT NULL,
    description    TEXT          NOT NULL,
    o_cloud_id     UUID          NOT NULL,
    url            VARCHAR(255)  NOT NULL,
    locations VARCHAR(64)[] NOT NULL,
    capabilities   json          NULL,
    capacity_info  json          NULL,
    extensions     json          NULL,
    data_source_id        UUID NOT NULL,
    generation_id  INTEGER       NOT NULL DEFAULT 0,
    external_id    VARCHAR(255),                             -- FQDN of resource in downstream data source (e.g., id=XXX)
    created_at     TIMESTAMPTZ            DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (data_source_id) REFERENCES data_source (data_source_id) -- Manual cascade required for events
);

-- Table: event
-- Description:  outbox pattern table
CREATE TABLE IF NOT EXISTS data_change_event
(
    data_change_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_type  VARCHAR(64) NOT NULL, -- Table name reference
    object_id    UUID        NOT NUll, -- Primary key
    parent_id      UUID NULL,
    before_state json        NULL,
    after_state json NULL,
    sequence_id SERIAL,                -- track insertion order rather than rely on timestamp since precision may cause ambiguity
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Table: subscription
CREATE TABLE subscription
(
    subscription_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_subscription_id UUID,
    filter                   TEXT,
    callback                 TEXT    NOT NULL,
    event_cursor             INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_callback UNIQUE (callback)
);

-- Table: alarm_dictionary
CREATE TABLE alarm_dictionary
(
    alarm_dictionary_id             UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    alarm_dictionary_version        VARCHAR(50)   NOT NULL,
    alarm_dictionary_schema_version VARCHAR(50)   NOT NULL,
    entity_type                     VARCHAR(255)  NOT NULL,
    vendor                          VARCHAR(255)  NOT NULL,
    management_interface_id         VARCHAR(50)[] DEFAULT ARRAY ['O2IMS']::VARCHAR[],
    pk_notification_field           TEXT[]        DEFAULT ARRAY ['alarmDefinitionID']::TEXT[],

    resource_type_id                UUID          NOT NULL UNIQUE,
    created_at                      TIMESTAMPTZ   DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (resource_type_id) REFERENCES resource_type (resource_type_id) ON DELETE CASCADE
);

-- Table: alarm_definition
CREATE TABLE alarm_definition
(
    alarm_definition_id     UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    alarm_name              VARCHAR(255)  NOT NULL,
    alarm_last_change       VARCHAR(50)   NOT NULL,
    alarm_change_type       VARCHAR(20)   NOT NULL,
    alarm_description       TEXT          NOT NULL,
    proposed_repair_actions TEXT          NOT NULL,
    clearing_type           VARCHAR(20)   NOT NULL,
    management_interface_id VARCHAR(50)[] DEFAULT ARRAY ['O2IMS']::VARCHAR[],
    pk_notification_field   TEXT[]        DEFAULT ARRAY ['alarmDefinitionID']::TEXT[],
    alarm_additional_fields JSONB,

    -- There exists alerts within the same PrometheusRule.Group that have the same name but different severity label.
    -- By adding this columns and a unique constraint on (alarm_name, severity), we can differentiate between them.
    -- All the Alerts from the Core Platform Monitoring have a severity label (except alert Watchdog). Alerts without a severity label are not affected by this.
    severity                VARCHAR(50)   NOT NULL,

    alarm_dictionary_id     UUID          NULL,

    created_at              TIMESTAMPTZ   DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (alarm_dictionary_id) REFERENCES alarm_dictionary (alarm_dictionary_id) ON DELETE CASCADE,
    CONSTRAINT unique_alarm UNIQUE(alarm_dictionary_id, alarm_name, severity)
);

-- Trigger function: Notify when resource_type changes
CREATE OR REPLACE FUNCTION notify_resource_type_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('resource_type_changed',
        json_build_object(
            'resource_type_id', COALESCE(NEW.resource_type_id, OLD.resource_type_id),
            'change_type', CASE
                WHEN TG_OP = 'INSERT' THEN 'created'
                WHEN TG_OP = 'DELETE' THEN 'deleted'
                ELSE 'updated'
            END
        )::text
    );
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

-- Create trigger on resource_type table for INSERT/UPDATE/DELETE
CREATE TRIGGER resource_type_change_trigger
AFTER INSERT OR UPDATE OR DELETE ON resource_type
FOR EACH ROW
EXECUTE FUNCTION notify_resource_type_change();
