-- 000009_add_region_to_cloud_environments.up.sql
-- Adds a primary region column to cloud_environments.
-- This tells the Auditing Service which AWS/GCP/Azure region to target when
-- making API calls (EC2 is region-scoped; without this the SDK defaults to
-- whatever .aws/config says, which is almost always wrong for client accounts).

ALTER TABLE cloud_environments
    ADD COLUMN IF NOT EXISTS region TEXT NOT NULL DEFAULT 'us-east-1';

COMMENT ON COLUMN cloud_environments.region IS 'Primary cloud region for this environment (e.g. us-east-1, eu-west-1).';
