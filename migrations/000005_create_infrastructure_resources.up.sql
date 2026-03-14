-- 000005_create_infrastructure_resources.up.sql

CREATE TABLE IF NOT EXISTS infrastructure_resources (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id      UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    environment_id       UUID        NOT NULL REFERENCES cloud_environments(id) ON DELETE CASCADE,
    provider_resource_id TEXT        NOT NULL,          -- cloud-native ID (e.g. i-0abc123, projects/my-proj/...)
    resource_type        TEXT        NOT NULL,          -- e.g. aws:ec2:instance, gcp:compute:disk
    attributes           JSONB       NOT NULL DEFAULT '{}',  -- flexible cloud-specific metadata
    status               TEXT        NOT NULL DEFAULT 'unknown',
    last_audited_at      TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A given cloud resource should only appear once per environment
    CONSTRAINT uq_resource_env_provider_id UNIQUE (environment_id, provider_resource_id)
);

-- Partial GIN index for JSONB attribute queries (e.g., filter by region, tags)
CREATE INDEX IF NOT EXISTS idx_infra_resources_attributes ON infrastructure_resources USING GIN (attributes);
CREATE INDEX IF NOT EXISTS idx_infra_resources_org_id     ON infrastructure_resources (organization_id);
CREATE INDEX IF NOT EXISTS idx_infra_resources_env_id     ON infrastructure_resources (environment_id);
CREATE INDEX IF NOT EXISTS idx_infra_resources_type       ON infrastructure_resources (resource_type);

COMMENT ON TABLE infrastructure_resources IS 'Discovered cloud resources, refreshed by the Auditing Service.';
COMMENT ON COLUMN infrastructure_resources.attributes IS 'JSONB blob of provider-specific attributes: region, tags, sizing, etc.';

-- ─── Row-Level Security ────────────────────────────────────────────────────────
ALTER TABLE infrastructure_resources ENABLE ROW LEVEL SECURITY;
ALTER TABLE infrastructure_resources FORCE ROW LEVEL SECURITY;

CREATE POLICY infra_resources_isolation_policy ON infrastructure_resources
    USING (organization_id = current_setting('app.current_organization_id')::UUID);

CREATE POLICY infra_resources_service_bypass_policy ON infrastructure_resources
    TO cmp_service
    USING (true);

COMMENT ON POLICY infra_resources_isolation_policy ON infrastructure_resources
    IS 'Restricts row access to the organization set in app.current_organization_id session variable.';
