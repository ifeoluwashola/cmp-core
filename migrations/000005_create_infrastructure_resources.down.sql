-- 000005_create_infrastructure_resources.down.sql

ALTER TABLE infrastructure_resources DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS infra_resources_service_bypass_policy ON infrastructure_resources;
DROP POLICY IF EXISTS infra_resources_isolation_policy      ON infrastructure_resources;
DROP TABLE IF EXISTS infrastructure_resources;
