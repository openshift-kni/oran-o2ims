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
    resource_kind    INTEGER      NOT NULL,                           -- enum of physical, logical, etc...
    resource_class   INTEGER      NOT NULL,                           -- enum of compute, networking, storage, etc...
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
    global_location_id VARCHAR(64) NOT NULL,
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
    locations      VARCHAR(64)[] NOT NULL,
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
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Table: subscription
CREATE TABLE subscription
(
    subscription_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_subscription_id VARCHAR(64),
    filter                   TEXT,
    callback                 TEXT    NOT NULL,
    event_cursor             INTEGER NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ      DEFAULT CURRENT_TIMESTAMP
);

-- Table: cached_alarm_dictionary
CREATE TABLE cached_alarm_dictionary
(
    alarm_dictionary_id             UUID PRIMARY KEY,
    resource_type_id                UUID         NOT NULL,
    alarm_dictionary_version        VARCHAR(50)  NOT NULL,
    alarm_dictionary_schema_version VARCHAR(50)  NOT NULL,
    entity_type                     VARCHAR(255) NOT NULL,
    vendor                          VARCHAR(255) NOT NULL,
    management_interface_id VARCHAR(50)[] DEFAULT ARRAY ['O2IMS']::VARCHAR[],
    pk_notification_field   TEXT[]        DEFAULT ARRAY ['alarm_dictionary_id']::TEXT[],
    created_at              TIMESTAMPTZ   DEFAULT CURRENT_TIMESTAMP
);

-- Table: cached_alarm_definition
CREATE TABLE cached_alarm_definition
(
    alarm_definition_id     UUID PRIMARY KEY,
    alarm_dictionary_id     UUID         NOT NULL,
    alarm_name              VARCHAR(255) NOT NULL,
    alarm_last_change       VARCHAR(50)  NOT NULL,
    alarm_description       TEXT         NOT NULL,
    proposed_repair_actions TEXT         NOT NULL,
    alarm_additional_fields JSONB,
    alarm_change_type       INTEGER      NOT NULL,
    clearing_type           INTEGER      NOT NULL,
    management_interface_id VARCHAR(50)[] DEFAULT ARRAY ['O2IMS']::VARCHAR[],
    pk_notification_field   TEXT[]        DEFAULT ARRAY ['alarm_definition_id']::TEXT[],
    created_at              TIMESTAMPTZ   DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (alarm_dictionary_id) REFERENCES cached_alarm_dictionary (alarm_dictionary_id),
    CONSTRAINT unique_alarm_name_last_change UNIQUE (alarm_name, alarm_last_change)
);
