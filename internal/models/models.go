// internal/models/models.go
// Go representations of every database table in cmp-core.
// These are plain data-carrier structs: no business logic, no DB calls.

package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── Enum types ───────────────────────────────────────────────────────────────
// Mirror the PostgreSQL ENUM definitions so the application layer can use
// typed constants instead of raw strings.

type UserRole string

const (
	UserRoleOwner    UserRole = "owner"
	UserRoleAdmin    UserRole = "admin"
	UserRoleEngineer UserRole = "engineer"
	UserRoleViewer   UserRole = "viewer"
)

type CloudProvider string

const (
	CloudProviderAWS   CloudProvider = "aws"
	CloudProviderGCP   CloudProvider = "gcp"
	CloudProviderAzure CloudProvider = "azure"
	CloudProviderOther CloudProvider = "other"
)

type AuthType string

const (
	AuthTypeOIDC               AuthType = "oidc"
	AuthTypeIAMCrossAccountRole AuthType = "iam_cross_account_role"
	AuthTypeServiceAccountKey  AuthType = "service_account_key"
)

type ConnStatus string

const (
	ConnStatusActive   ConnStatus = "active"
	ConnStatusInactive ConnStatus = "inactive"
	ConnStatusError    ConnStatus = "error"
	ConnStatusPending  ConnStatus = "pending"
)

// ─── Organization ─────────────────────────────────────────────────────────────
// Top-level tenant entity. No RLS — access is controlled by JWT org claim.

type Organization struct {
	ID        uuid.UUID `db:"id"         json:"id"`
	Name      string    `db:"name"       json:"name"`
	Tier      string    `db:"tier"       json:"tier"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// ─── User ─────────────────────────────────────────────────────────────────────
// Platform user scoped to a single organization. RLS-protected.

type User struct {
	ID             uuid.UUID `db:"id"              json:"id"`
	OrganizationID uuid.UUID `db:"organization_id" json:"organization_id"`
	Email          string    `db:"email"           json:"email"`
	Role           UserRole  `db:"role"            json:"role"`
	// PasswordHash is excluded from JSON responses deliberately.
	PasswordHash string    `db:"password_hash" json:"-"`
	CreatedAt    time.Time `db:"created_at"    json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"    json:"updated_at"`
}

// ─── CloudEnvironment ─────────────────────────────────────────────────────────
// A spoke cloud account integrated into the platform. RLS-protected.

type CloudEnvironment struct {
	ID               uuid.UUID     `db:"id"                json:"id"`
	OrganizationID   uuid.UUID     `db:"organization_id"   json:"organization_id"`
	Name             string        `db:"name"              json:"name"`
	Provider         CloudProvider `db:"provider"          json:"provider"`
	AuthType         AuthType      `db:"auth_type"         json:"auth_type"`
	// RoleARN is nullable — not all auth types require it.
	RoleARN          *string       `db:"role_arn"          json:"role_arn,omitempty"`
	ConnectionStatus ConnStatus    `db:"connection_status" json:"connection_status"`
	CreatedAt        time.Time     `db:"created_at"        json:"created_at"`
	UpdatedAt        time.Time     `db:"updated_at"        json:"updated_at"`
}

// ─── InfrastructureResource ───────────────────────────────────────────────────
// A discovered cloud resource refreshed by the Auditing Service. RLS-protected.
// Attributes maps to a JSONB column — stored as json.RawMessage so callers can
// unmarshal into provider-specific structs without an extra allocation.

type InfrastructureResource struct {
	ID                 uuid.UUID       `db:"id"                   json:"id"`
	OrganizationID     uuid.UUID       `db:"organization_id"       json:"organization_id"`
	EnvironmentID      uuid.UUID       `db:"environment_id"        json:"environment_id"`
	ProviderResourceID string          `db:"provider_resource_id" json:"provider_resource_id"`
	ResourceType       string          `db:"resource_type"        json:"resource_type"`
	// Attributes holds arbitrary cloud-provider metadata (region, tags, sizing…).
	Attributes         json.RawMessage `db:"attributes"           json:"attributes"`
	Status             string          `db:"status"               json:"status"`
	// LastAuditedAt is nullable — newly discovered resources have not been audited.
	LastAuditedAt      *time.Time      `db:"last_audited_at"      json:"last_audited_at,omitempty"`
	CreatedAt          time.Time       `db:"created_at"           json:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"           json:"updated_at"`
}

// ─── DailyCost ────────────────────────────────────────────────────────────────
// Daily billing record ingested by the FinOps Engine. RLS-protected.
// Amount uses string to preserve the full NUMERIC(18,6) precision returned by
// pgx without floating-point rounding.

type DailyCost struct {
	ID              uuid.UUID `db:"id"               json:"id"`
	OrganizationID  uuid.UUID `db:"organization_id"  json:"organization_id"`
	EnvironmentID   uuid.UUID `db:"environment_id"   json:"environment_id"`
	Date            time.Time `db:"date"             json:"date"`
	ServiceCategory string    `db:"service_category" json:"service_category"`
	// Amount is a string representation of NUMERIC(18,6) to avoid float64 loss.
	Amount          string    `db:"amount"           json:"amount"`
	Currency        string    `db:"currency"         json:"currency"`
	CreatedAt       time.Time `db:"created_at"       json:"created_at"`
}
