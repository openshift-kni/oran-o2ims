CREATE DATABASE resource_server;

-- Table: data_source
CREATE TABLE IF NOT EXISTS data_source (
    id            SERIAL PRIMARY KEY,
    name          VARCHAR(255) NOT NULL,
    generation_id INTEGER      NOT NULL DEFAULT 0,                 -- incremented on each audit/sync
    last_snapshot TIMESTAMPTZ           DEFAULT CURRENT_TIMESTAMP, -- last completed snapshot
    created_at    TIMESTAMPTZ           DEFAULT CURRENT_TIMESTAMP  -- TBD; tracks when first imported
);

-- Table: resource_type
CREATE TABLE IF NOT EXISTS resource_type (
    resource_type_id UUID PRIMARY KEY,
    name           VARCHAR(255) NOT NULL,
    description    TEXT         NULL,
    vendor         VARCHAR(64)  NOT NULL,
    model          VARCHAR(64)  NOT NULL,
    version        VARCHAR(64)  NOT NULL,
    resource_kind  INTEGER      NOT NULL,                           -- enum of physical, logical, etc...
    resource_class INTEGER      NOT NULL,                           -- enum of compute, networking, storage, etc...
    extensions     json         NULL,
    data_source_id INTEGER      NOT NULL,
    generation_id  INTEGER      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ           DEFAULT CURRENT_TIMESTAMP, -- TBD; tracks when first imported
    FOREIGN KEY (data_source_id) REFERENCES data_source (id)        -- Manual cascade required for events
);

-- Table: resource_pool
CREATE TABLE IF NOT EXISTS resource_pool (
    resource_pool_id UUID PRIMARY KEY,
    global_location_id VARCHAR(64)  NOT NULL,
    name               VARCHAR(255) NOT NULL,
    description        TEXT         NULL,
    o_cloud_id         UUID         NOT NULL,
    location           VARCHAR(64)  NOT NULL,
    extensions         json         NULL,
    data_source_id     INTEGER      NOT NULL,
    generation_id      INTEGER      NOT NULL DEFAULT 0,
    external_id      VARCHAR(255),                           -- FQDN of resource in downstream data source (e.g., id=XXX)
    created_at       TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,  -- TBD; tracks when first imported
    FOREIGN KEY (data_source_id) REFERENCES data_source (id) -- Manual cascade required for events
);

-- Table: resource
CREATE TABLE IF NOT EXISTS resource (
    resource_id UUID PRIMARY KEY,
    description      TEXT         NULL,
    resource_type_id UUID         NOT NULL,
    global_asset_id  VARCHAR(255) NULL,
    resource_pool_id UUID         NULL,
    extensions       json         NULL,
    data_source_id   INTEGER      NOT NULL,
    generation_id    INTEGER      NOT NULL DEFAULT 0,
    external_id VARCHAR(255),                                -- FQDN of resource in downstream data source (e.g., id=XXX)
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,       -- TBD; tracks when first imported
    FOREIGN KEY (data_source_id) REFERENCES data_source (id) -- Manual cascade required for events
);

-- Table: resource_pool_member
CREATE TABLE IF NOT EXISTS resource_pool_member (
    resource_pool_id UUID NOT NULL,
    resource_id UUID NOT NULL,
    PRIMARY KEY (resource_pool_id, resource_id),
    FOREIGN KEY (resource_pool_id) REFERENCES resource_pool (resource_pool_id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resource (resource_id) ON DELETE CASCADE
);

-- Table: deployment_manager
CREATE TABLE IF NOT EXISTS deployment_manager (
    cluster_id  UUID PRIMARY KEY,
    name           VARCHAR(255) NOT NULL,
    description    TEXT         NULL,
    o_cloud_id     UUID         NOT NULL,
    url            VARCHAR(255) NOT NULL,
    locations      TEXT         NULL,
    capabilities   TEXT         NULL,
    capacity_info  TEXT         NULL,
    extensions     json         NULL,
    data_source_id INTEGER      NOT NULL,
    generation_id  INTEGER      NOT NULL DEFAULT 0,
    external_id VARCHAR(255),                                -- FQDN of resource in downstream data source (e.g., id=XXX)
    created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (data_source_id) REFERENCES data_source (id) -- Manual cascade required for events
);

-- Table: event
-- Description:  outbox pattern table
CREATE TABLE IF NOT EXISTS event (
    id SERIAL PRIMARY KEY,
    object_type  VARCHAR(64) NOT NULL, -- Table name reference
    object_id    UUID        NOT NUll, -- Primary key
    before_state json        NULL,
    after_state  json        NULL
);

-- Table: subscription
DROP TABLE IF EXISTS subscription CASCADE;
CREATE TABLE subscription (
    subscription_id          UUID PRIMARY KEY,
    consumer_subscription_id UUID,
    filter                   TEXT,
    callback                 TEXT    NOT NULL,
    event_cursor             INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

