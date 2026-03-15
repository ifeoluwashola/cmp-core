ALTER TABLE daily_costs 
ADD CONSTRAINT unique_daily_cost_per_env 
UNIQUE (organization_id, environment_id, date, service_category);