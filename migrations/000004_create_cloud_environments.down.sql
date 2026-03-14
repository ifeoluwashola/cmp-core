-- 000004_create_cloud_environments.down.sql

ALTER TABLE cloud_environments DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS cloud_envs_service_bypass_policy ON cloud_environments;
DROP POLICY IF EXISTS cloud_envs_isolation_policy      ON cloud_environments;
DROP TABLE IF EXISTS cloud_environments;
DROP TYPE IF EXISTS conn_status;
DROP TYPE IF EXISTS auth_type;
DROP TYPE IF EXISTS cloud_provider;
