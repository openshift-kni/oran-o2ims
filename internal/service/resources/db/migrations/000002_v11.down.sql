-- =============================================================================
-- Rollback V11 Schema Migration
-- O-RAN.WG6.TS.O2IMS-INTERFACE-R005-v11.00
--
-- This rollback:
-- 1. Restores deprecated columns to resource_pool
-- 2. Removes o_cloud_site_id from resource_pool
-- 3. Drops o_cloud_site and location tables
-- =============================================================================


--  Drop indexes first
--
DROP INDEX IF EXISTS idx_resource_pool_o_cloud_site_id;
DROP INDEX IF EXISTS idx_o_cloud_site_global_location_id;

-- Restore deprecated columns to resource_pool

-- Restore global_location_id (was UUID NOT NULL)
ALTER TABLE resource_pool ADD COLUMN IF NOT EXISTS global_location_id UUID;
-- Restore o_cloud_id (was UUID NOT NULL)
ALTER TABLE resource_pool ADD COLUMN IF NOT EXISTS o_cloud_id UUID;
-- Restore location (was VARCHAR(64) NULL)
ALTER TABLE resource_pool ADD COLUMN IF NOT EXISTS location VARCHAR(64);

-- Remove o_cloud_site_id from resource_pool
ALTER TABLE resource_pool DROP CONSTRAINT IF EXISTS fk_resource_pool_site;
ALTER TABLE resource_pool DROP COLUMN IF EXISTS o_cloud_site_id;

-- Drop v11 tables

-- Drop o_cloud_site table first (depends on location)
DROP TABLE IF EXISTS o_cloud_site;

-- Drop location table (no dependencies)
-- NOTE: chk_location_address_required constraint is dropped automatically with the table
DROP TABLE IF EXISTS location;
