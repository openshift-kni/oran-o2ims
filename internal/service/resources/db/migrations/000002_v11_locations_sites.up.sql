-- =============================================================================
-- V11 Locations and Sites Feature
-- O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
-- Creates location and o_cloud_site tables, adds o_cloud_site_id to resource_pool
-- =============================================================================

-- Table: location
-- Represents a physical or logical location where O-Cloud Sites can be deployed
CREATE TABLE IF NOT EXISTS location
(
    global_location_id VARCHAR(255) PRIMARY KEY,                      -- SMO-defined identifier (not UUID per spec)
    name               VARCHAR(255) NOT NULL,
    description        TEXT         NOT NULL,
    coordinate         JSONB        NULL,                             -- GeoJSON Point: {"type": "Point", "coordinates": [lon, lat]}
    civic_address      JSONB        NULL,                             -- Array of {caType, caValue} per RFC 4776
    address            VARCHAR(512) NULL,                             -- Human-readable address
    extensions         JSONB        NULL,
    data_source_id     UUID         NOT NULL,
    generation_id      INTEGER      NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ  DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (data_source_id) REFERENCES data_source (data_source_id),
    -- Per O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00 3.2.6.2.16: at least one of coordinate, civic_address, or address is required
    CONSTRAINT chk_location_address_required
        CHECK (coordinate IS NOT NULL OR civic_address IS NOT NULL OR address IS NOT NULL)
);

-- Table: o_cloud_site
-- Represents an O-Cloud site instance at a location
CREATE TABLE IF NOT EXISTS o_cloud_site
(
    o_cloud_site_id    UUID PRIMARY KEY,                              -- Locally unique within O-Cloud
    global_location_id VARCHAR(255) NOT NULL,                         -- References location
    name               VARCHAR(255) NOT NULL,
    description        TEXT         NOT NULL,
    extensions         JSONB        NULL,
    data_source_id     UUID         NOT NULL,
    generation_id      INTEGER      NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ  DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (data_source_id) REFERENCES data_source (data_source_id),
    FOREIGN KEY (global_location_id) REFERENCES location (global_location_id)
);

-- Alter resource_pool to reference o_cloud_site
-- Column is NULLABLE for backward compatibility with existing data
ALTER TABLE resource_pool ADD COLUMN IF NOT EXISTS o_cloud_site_id UUID NULL;
ALTER TABLE resource_pool ADD CONSTRAINT fk_resource_pool_site 
    FOREIGN KEY (o_cloud_site_id) REFERENCES o_cloud_site (o_cloud_site_id);

-- Indexes for foreign key columns

-- Index on o_cloud_site.global_location_id for location -> sites lookups
CREATE INDEX IF NOT EXISTS idx_o_cloud_site_global_location_id
    ON o_cloud_site (global_location_id);

-- Index on resource_pool.o_cloud_site_id for site -> pools lookups
CREATE INDEX IF NOT EXISTS idx_resource_pool_o_cloud_site_id
    ON resource_pool (o_cloud_site_id);
