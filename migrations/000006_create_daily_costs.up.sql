-- 000006_create_daily_costs.up.sql

CREATE TABLE IF NOT EXISTS daily_costs (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    environment_id   UUID        NOT NULL REFERENCES cloud_environments(id) ON DELETE CASCADE,
    date             DATE        NOT NULL,
    service_category TEXT        NOT NULL,    -- e.g. Compute, Storage, Networking, Database
    amount           NUMERIC(18,6) NOT NULL,  -- high precision for sub-cent cloud billing
    currency         CHAR(3)     NOT NULL DEFAULT 'USD',   -- ISO 4217
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Prevent duplicate ingestion for the same org/env/date/service
    CONSTRAINT uq_daily_costs_env_date_service UNIQUE (environment_id, date, service_category)
);

CREATE INDEX IF NOT EXISTS idx_daily_costs_org_id  ON daily_costs (organization_id);
CREATE INDEX IF NOT EXISTS idx_daily_costs_env_id  ON daily_costs (environment_id);
CREATE INDEX IF NOT EXISTS idx_daily_costs_date    ON daily_costs (date DESC);
CREATE INDEX IF NOT EXISTS idx_daily_costs_service ON daily_costs (service_category);

COMMENT ON TABLE daily_costs IS 'Daily cost records ingested by the FinOps Engine from cloud billing APIs.';
COMMENT ON COLUMN daily_costs.amount   IS 'Billing amount with 6 decimal places to capture sub-cent precision.';
COMMENT ON COLUMN daily_costs.currency IS 'ISO 4217 currency code (e.g., USD, EUR).';

-- ─── Row-Level Security ────────────────────────────────────────────────────────
ALTER TABLE daily_costs ENABLE ROW LEVEL SECURITY;
ALTER TABLE daily_costs FORCE ROW LEVEL SECURITY;

CREATE POLICY daily_costs_isolation_policy ON daily_costs
    USING (organization_id = current_setting('app.current_organization_id')::UUID);

CREATE POLICY daily_costs_service_bypass_policy ON daily_costs
    TO cmp_service
    USING (true);

COMMENT ON POLICY daily_costs_isolation_policy ON daily_costs
    IS 'Restricts row access to the organization set in app.current_organization_id session variable.';
