-- 000008_create_deployments.up.sql
-- Deployments table tracks IaC pipeline runs triggered by the Provisioning Engine.

CREATE TABLE IF NOT EXISTS deployments (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID        NOT NULL REFERENCES organizations(id)       ON DELETE CASCADE,
    environment_id  UUID        NOT NULL REFERENCES cloud_environments(id)  ON DELETE CASCADE,
    module_name     TEXT        NOT NULL,                   -- e.g. base-vpc, eks-cluster
    status          TEXT        NOT NULL DEFAULT 'queued',  -- queued | running | success | failed
    job_id          TEXT,                                   -- CI/CD tracking ID (set after trigger)
    logs            TEXT,                                   -- captured pipeline output
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deployments_org_id ON deployments (organization_id);
CREATE INDEX IF NOT EXISTS idx_deployments_env_id ON deployments (environment_id);
CREATE INDEX IF NOT EXISTS idx_deployments_job_id ON deployments (job_id);
CREATE INDEX IF NOT EXISTS idx_deployments_status ON deployments (status);

COMMENT ON TABLE  deployments            IS 'IaC pipeline runs triggered by the Provisioning Engine.';
COMMENT ON COLUMN deployments.module_name IS 'Terraform/OpenTofu module to apply (e.g. base-vpc).';
COMMENT ON COLUMN deployments.job_id      IS 'Opaque tracking ID returned by the CI/CD provider after trigger.';
COMMENT ON COLUMN deployments.logs        IS 'Terminal output captured from the pipeline run.';

-- ─── Row-Level Security ────────────────────────────────────────────────────────
ALTER TABLE deployments ENABLE ROW LEVEL SECURITY;
ALTER TABLE deployments FORCE ROW LEVEL SECURITY;

-- Tenant isolation: users only see their own org's deployments.
CREATE POLICY deployments_isolation_policy ON deployments
    USING (organization_id = current_setting('app.current_organization_id')::UUID);

-- Service bypass: the webhook handler (running as cmp_service) resolves
-- any deployment by job_id across all orgs before re-scoping to WithOrgTx.
CREATE POLICY deployments_service_bypass_policy ON deployments
    TO cmp_service
    USING (true);

COMMENT ON POLICY deployments_isolation_policy     ON deployments IS 'Tenant RLS: restrict to current org.';
COMMENT ON POLICY deployments_service_bypass_policy ON deployments IS 'Service bypass for webhook cross-org job_id lookup.';
