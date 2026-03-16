-- 000010_regions_array_on_cloud_environments.down.sql

ALTER TABLE cloud_environments DROP COLUMN IF EXISTS regions;
ALTER TABLE cloud_environments ADD COLUMN region TEXT NOT NULL DEFAULT 'us-east-1';
