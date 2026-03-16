// internal/repository/cloud_env.go
// Repository methods for the cloud_environments table.
//
// All methods accept a *pgx.Tx injected by database.WithOrgTx, which has
// already executed SET LOCAL app.current_organization_id so RLS applies
// automatically. No WHERE organization_id = ? is needed in these queries.

package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/jackc/pgx/v5"
)

// CloudEnvRepository holds no state — it is a thin wrapper around SQL.
// Methods are on a value receiver so callers can embed or mock as needed.
type CloudEnvRepository struct{}

// NewCloudEnvRepository constructs a CloudEnvRepository.
func NewCloudEnvRepository() *CloudEnvRepository {
	return &CloudEnvRepository{}
}

// CreateInput carries the fields required to register a new cloud environment.
type CreateCloudEnvInput struct {
	OrganizationID uuid.UUID
	Name           string
	Provider       models.CloudProvider
	AuthType       models.AuthType
	RoleARN        *string  // optional
	Regions        []string // cloud regions to audit; nil → provider default
}

// defaultRegions returns the sensible default region list for a given cloud provider.
func defaultRegions(p models.CloudProvider) []string {
	switch p {
	case models.CloudProviderGCP:
		return []string{"us-central1"}
	case models.CloudProviderAzure:
		return []string{"eastus"}
	default: // aws, other
		return []string{"us-east-1"}
	}
}

// Create inserts a new cloud environment and returns the fully populated row.
// RLS allows the INSERT because the session's organization_id matches the row.
func (r *CloudEnvRepository) Create(ctx context.Context, tx pgx.Tx, in CreateCloudEnvInput) (*models.CloudEnvironment, error) {
	regions := in.Regions
	if len(regions) == 0 {
		regions = defaultRegions(in.Provider)
	}
	const q = `
		INSERT INTO cloud_environments
			(organization_id, name, provider, auth_type, role_arn, regions, connection_status)
		VALUES
			($1, $2, $3, $4, $5, $6, 'pending')
		RETURNING
			id, organization_id, name, provider, auth_type,
			role_arn, regions, connection_status, created_at, updated_at`

	row := tx.QueryRow(ctx, q,
		in.OrganizationID,
		in.Name,
		string(in.Provider),
		string(in.AuthType),
		in.RoleARN,
		regions,
	)

	env, err := scanCloudEnv(row)
	if err != nil {
		return nil, fmt.Errorf("repository: create cloud env: %w", err)
	}
	return env, nil
}

// List returns all cloud environments visible to the current organization.
// The RLS policy filters the rows — no extra WHERE clause is required.
func (r *CloudEnvRepository) List(ctx context.Context, tx pgx.Tx) ([]*models.CloudEnvironment, error) {
	const q = `
		SELECT
			id, organization_id, name, provider, auth_type,
			role_arn, regions, connection_status, created_at, updated_at
		FROM cloud_environments
		ORDER BY created_at DESC`

	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("repository: list cloud envs: %w", err)
	}
	defer rows.Close()

	var envs []*models.CloudEnvironment
	for rows.Next() {
		env, err := scanCloudEnv(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan cloud env: %w", err)
		}
		envs = append(envs, env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate cloud envs: %w", err)
	}
	return envs, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// pgxScanner is satisfied by both pgx.Row and pgx.Rows.
type pgxScanner interface {
	Scan(dest ...any) error
}

func scanCloudEnv(s pgxScanner) (*models.CloudEnvironment, error) {
	var (
		env      models.CloudEnvironment
		provider string
		authType string
		status   string
		roleARN  *string
		createdAt, updatedAt time.Time
	)
	err := s.Scan(
		&env.ID,
		&env.OrganizationID,
		&env.Name,
		&provider,
		&authType,
		&roleARN,
		&env.Regions, // pgx scans TEXT[] directly into []string
		&status,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}
	env.Provider = models.CloudProvider(provider)
	env.AuthType = models.AuthType(authType)
	env.ConnectionStatus = models.ConnStatus(status)
	env.RoleARN = roleARN
	env.CreatedAt = createdAt
	env.UpdatedAt = updatedAt
	return &env, nil
}
