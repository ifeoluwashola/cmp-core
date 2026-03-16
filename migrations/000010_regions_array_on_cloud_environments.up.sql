-- 000010_regions_array_on_cloud_environments.up.sql
-- Replaces the single `region` TEXT column with `regions TEXT[]` so each
-- cloud environment can be audited across multiple cloud regions.

ALTER TABLE cloud_environments
    DROP COLUMN IF EXISTS region;

ALTER TABLE cloud_environments
    ADD COLUMN regions TEXT[] NOT NULL DEFAULT ARRAY['us-east-1'];

COMMENT ON COLUMN cloud_environments.regions IS 'List of cloud regions to audit for this environment (e.g. {us-east-1,eu-west-1}).';
