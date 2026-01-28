-- =============================================================================
-- Rollback V11 Locations and Sites Feature
-- O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
-- =============================================================================

-- 1. Remove FK constraint and column from resource_pool (reverse of up migration to respect FK constraints, order matters)
ALTER TABLE resource_pool DROP CONSTRAINT IF EXISTS fk_resource_pool_site;
ALTER TABLE resource_pool DROP COLUMN IF EXISTS o_cloud_site_id;

-- 2. Drop o_cloud_site table (depends on location)
DROP TABLE IF EXISTS o_cloud_site;

-- 3. Drop location table (no dependencies)
DROP TABLE IF EXISTS location;
