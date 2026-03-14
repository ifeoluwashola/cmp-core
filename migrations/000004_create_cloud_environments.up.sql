-- 000004_create_cloud_environments.up.sql

-- ─── ENUM types ────────────────────────────────────────────────────────────────
CREATE TYPE cloud_provider AS ENUM ('aws', 'gcp', 'azure', 'other');
CREATE TYPE auth_type      AS ENUM ('oidc', 'iam_cross_account_role', 'service_account_key');
CREATE TYPE conn_status    AS ENUM ('active', 'inactive', 'error', 'pending');

-- ─── Table ────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cloud_environments (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name              TEXT        NOT NULL,
    provider          cloud_provider NOT NULL,
    auth_type         auth_type   NOT NULL,
    role_arn          TEXT,                          -- AWS cross-account role ARN / GCP WI pool / Azure SP
    connection_status conn_status NOT NULL DEFAULT 'pending',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_cloud_env_org_name UNIQUE (organization_id, name)
);

CREATE INDEX IF NOT EXISTS idx_cloud_envs_org_id ON cloud_environments (organization_id);

COMMENT ON TABLE cloud_environments IS 'Spoke connections: one row per cloud account integrated into the platform.';
COMMENT ON COLUMN cloud_environments.role_arn IS 'AWS role ARN, GCP WI pool resource, or Azure SP identifier used for OIDC/STS auth.';

-- ─── Row-Level Security ────────────────────────────────────────────────────────
ALTER TABLE cloud_environments ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_environments FORCE ROW LEVEL SECURITY;

CREATE POLICY cloud_envs_isolation_policy ON cloud_environments
    USING (organization_id = current_setting('app.current_organization_id')::UUID);

CREATE POLICY cloud_envs_service_bypass_policy ON cloud_environments
    TO cmp_service
    USING (true);

COMMENT ON POLICY cloud_envs_isolation_policy ON cloud_environments
    IS 'Restricts row access to the organization set in app.current_organization_id session variable.';
