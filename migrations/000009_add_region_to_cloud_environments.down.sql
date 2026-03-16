-- 000009_add_region_to_cloud_environments.down.sql

ALTER TABLE cloud_environments DROP COLUMN IF EXISTS region;
