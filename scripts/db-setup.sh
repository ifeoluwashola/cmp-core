#!/usr/bin/env bash
# scripts/db-setup.sh
# ─────────────────────────────────────────────────────────────────────────────
# One-time database and role setup script.
# Run this BEFORE running 'make migrate-up'.
#
# Usage:
#   source .env && bash scripts/db-setup.sh
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

: "${DB_HOST:?DB_HOST not set}"
: "${DB_PORT:?DB_PORT not set}"
: "${DB_USER:?DB_USER not set}"
: "${DB_PASSWORD:?DB_PASSWORD not set}"
: "${DB_NAME:?DB_NAME not set}"

PSQL="psql -h $DB_HOST -p $DB_PORT -U $DB_USER"

# ── 1. Create database ────────────────────────────────────────────────────────
echo "==> Creating database '$DB_NAME' (if not exists)..."
$PSQL -d postgres -tc \
  "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME'" | \
  grep -q 1 || $PSQL -d postgres -c "CREATE DATABASE \"$DB_NAME\";"

# ── 2. Create application roles ───────────────────────────────────────────────
echo "==> Creating application roles..."

# cmp_app: the role used by every microservice for regular API-serving queries.
# It runs under RLS — it MUST set app.current_organization_id before querying.
$PSQL -d "$DB_NAME" -c "
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'cmp_app') THEN
    CREATE ROLE cmp_app NOLOGIN;
  END IF;
END
\$\$;
GRANT CONNECT ON DATABASE \"$DB_NAME\" TO cmp_app;
GRANT USAGE  ON SCHEMA public TO cmp_app;
"

# cmp_service: a privileged internal role that bypasses RLS.
# Used only by the Identity Service for cross-org lookups (e.g. login by email).
$PSQL -d "$DB_NAME" -c "
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'cmp_service') THEN
    CREATE ROLE cmp_service NOLOGIN;
  END IF;
END
\$\$;
GRANT CONNECT ON DATABASE \"$DB_NAME\" TO cmp_service;
GRANT USAGE  ON SCHEMA public TO cmp_service;
"

# ── 3. Set default privileges so future tables are auto-granted ───────────────
# ALTER DEFAULT PRIVILEGES applies to all tables/sequences created by DB_USER
# going forward (i.e. everything 'make migrate-up' creates).
echo "==> Setting default privileges for future tables and sequences..."
$PSQL -d "$DB_NAME" -c "
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO cmp_app, cmp_service;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO cmp_app, cmp_service;
"

echo ""
echo "✅  Database '$DB_NAME' and roles cmp_app / cmp_service are ready."
echo "    Next: make migrate-up"
