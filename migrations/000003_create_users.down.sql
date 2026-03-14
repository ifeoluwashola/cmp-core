-- 000003_create_users.down.sql

ALTER TABLE users DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS users_service_bypass_policy ON users;
DROP POLICY IF EXISTS users_isolation_policy ON users;
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS user_role;
