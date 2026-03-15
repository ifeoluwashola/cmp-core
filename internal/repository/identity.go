// internal/repository/identity.go
// Repository methods for user registration and login.
//
// Both methods run under WithServiceTx (cmp_service role) because:
//  - CreateOrganizationWithAdmin must INSERT across two RLS-protected tables
//    in one atomic transaction before any org context exists.
//  - GetUserByEmail must look up a user by email across all orgs (login flow
//    doesn't know the org until after the lookup).

package repository

import (
	"context"
	"fmt"

	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/jackc/pgx/v5"
)

// IdentityRepository handles user and organization creation/lookup.
type IdentityRepository struct{}

func NewIdentityRepository() *IdentityRepository { return &IdentityRepository{} }

// CreateOrganizationWithAdmin inserts a new Organization and an owner-role User
// atomically inside the provided service-level transaction.
// Returns pointers to both newly created records.
func (r *IdentityRepository) CreateOrganizationWithAdmin(
	ctx context.Context,
	tx pgx.Tx,
	orgName, email, passwordHash string,
) (*models.Organization, *models.User, error) {

	// ── 1. Insert organization ────────────────────────────────────────────────
	const orgQ = `
		INSERT INTO organizations (name, tier)
		VALUES ($1, 'standard')
		RETURNING id, name, tier, created_at`

	var org models.Organization
	if err := tx.QueryRow(ctx, orgQ, orgName).Scan(
		&org.ID, &org.Name, &org.Tier, &org.CreatedAt,
	); err != nil {
		return nil, nil, fmt.Errorf("identity: create org: %w", err)
	}

	// ── 2. Insert owner user ──────────────────────────────────────────────────
	const userQ = `
		INSERT INTO users (organization_id, email, role, password_hash)
		VALUES ($1, $2, 'owner', $3)
		RETURNING id, organization_id, email, role, created_at, updated_at`

	var user models.User
	var role string
	if err := tx.QueryRow(ctx, userQ, org.ID, email, passwordHash).Scan(
		&user.ID, &user.OrganizationID, &user.Email,
		&role, &user.CreatedAt, &user.UpdatedAt,
	); err != nil {
		return nil, nil, fmt.Errorf("identity: create admin user: %w", err)
	}
	user.Role = models.UserRole(role)

	return &org, &user, nil
}

// GetUserByEmail does a cross-org lookup by email — only valid under the
// cmp_service bypass policy since users are RLS-isolated per org.
func (r *IdentityRepository) GetUserByEmail(
	ctx context.Context,
	tx pgx.Tx,
	email string,
) (*models.User, error) {
	const q = `
		SELECT id, organization_id, email, role, password_hash, created_at, updated_at
		FROM   users
		WHERE  email = $1
		LIMIT  1`

	var user models.User
	var role string
	err := tx.QueryRow(ctx, q, email).Scan(
		&user.ID, &user.OrganizationID, &user.Email,
		&role, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("identity: user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("identity: get user by email: %w", err)
	}
	user.Role = models.UserRole(role)
	return &user, nil
}
