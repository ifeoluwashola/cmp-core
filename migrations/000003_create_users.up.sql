-- 000003_create_users.up.sql

-- ─── ENUM types ────────────────────────────────────────────────────────────────
CREATE TYPE user_role AS ENUM ('owner', 'admin', 'engineer', 'viewer');

-- ─── Table ────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email           TEXT        NOT NULL UNIQUE,
    role            user_role   NOT NULL DEFAULT 'viewer',
    password_hash   TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_organization_id ON users (organization_id);
CREATE INDEX IF NOT EXISTS idx_users_email           ON users (email);

COMMENT ON TABLE users IS 'Platform users scoped to an organization.';
COMMENT ON COLUMN users.role IS 'RBAC role: owner | admin | engineer | viewer';

-- ─── Row-Level Security ────────────────────────────────────────────────────────
-- Enable RLS on the table.
ALTER TABLE users ENABLE ROW LEVEL SECURITY;

-- Force RLS to apply even to the table owner (prevents accidental bypasses).
ALTER TABLE users FORCE ROW LEVEL SECURITY;

-- Policy: app_user (the application DB role) can only SELECT / UPDATE / DELETE
-- rows belonging to its current organization context, set via a session variable.
--
-- Usage from application code:
--   SET LOCAL app.current_organization_id = '<uuid>';
--
CREATE POLICY users_isolation_policy ON users
    USING (organization_id = current_setting('app.current_organization_id')::UUID);

-- Policy: service-to-service calls authenticated as 'cmp_service' role bypass RLS
-- (e.g., the identity service needs cross-org lookups during login).
CREATE POLICY users_service_bypass_policy ON users
    TO cmp_service
    USING (true);

COMMENT ON POLICY users_isolation_policy ON users
    IS 'Restricts row access to the organization set in app.current_organization_id session variable.';
