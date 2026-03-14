-- 000006_create_daily_costs.down.sql

ALTER TABLE daily_costs DISABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS daily_costs_service_bypass_policy ON daily_costs;
DROP POLICY IF EXISTS daily_costs_isolation_policy      ON daily_costs;
DROP TABLE IF EXISTS daily_costs;
