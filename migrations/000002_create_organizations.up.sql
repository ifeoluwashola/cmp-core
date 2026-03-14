-- 000002_create_organizations.up.sql
-- Organizations are the top-level tenants. No RLS needed here;
-- access is controlled at the application layer (JWT org claim checks).

CREATE TABLE IF NOT EXISTS organizations (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL,
    tier       TEXT        NOT NULL DEFAULT 'standard',  -- e.g., standard, enterprise
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_organizations_name ON organizations (name);

COMMENT ON TABLE organizations IS 'Top-level tenant entities. One row per client / MSP customer.';
COMMENT ON COLUMN organizations.tier IS 'Service tier: standard | professional | enterprise';
