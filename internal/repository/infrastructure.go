// internal/repository/infrastructure.go
// Repository methods for the infrastructure_resources table.
//
// All methods receive a *pgx.Tx already scoped by database.WithOrgTx, so RLS
// automatically constrains every query to the calling organization. No explicit
// WHERE organization_id = ? is needed.

package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/jackc/pgx/v5"
)

// InfrastructureRepository is a thin SQL wrapper for infrastructure_resources.
type InfrastructureRepository struct{}

// NewInfrastructureRepository constructs an InfrastructureRepository.
func NewInfrastructureRepository() *InfrastructureRepository {
	return &InfrastructureRepository{}
}

// ListResources returns infrastructure resources visible to the current
// organization (enforced by RLS). When envID is non-nil the result is further
// filtered to that specific cloud environment.
func (r *InfrastructureRepository) ListResources(
	ctx context.Context,
	tx pgx.Tx,
	envID *uuid.UUID,
) ([]*models.InfrastructureResource, error) {
	const baseQuery = `
		SELECT
			id, organization_id, environment_id,
			provider_resource_id, resource_type, attributes,
			status, last_audited_at, created_at, updated_at
		FROM infrastructure_resources`

	var (
		rows pgx.Rows
		err  error
	)

	if envID != nil {
		rows, err = tx.Query(ctx,
			baseQuery+" WHERE environment_id = $1 ORDER BY created_at DESC",
			*envID,
		)
	} else {
		rows, err = tx.Query(ctx, baseQuery+" ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, fmt.Errorf("repository: list resources: %w", err)
	}
	defer rows.Close()

	var resources []*models.InfrastructureResource
	for rows.Next() {
		var res models.InfrastructureResource
		if err := rows.Scan(
			&res.ID,
			&res.OrganizationID,
			&res.EnvironmentID,
			&res.ProviderResourceID,
			&res.ResourceType,
			&res.Attributes,
			&res.Status,
			&res.LastAuditedAt,
			&res.CreatedAt,
			&res.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository: scan resource: %w", err)
		}
		resources = append(resources, &res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate resources: %w", err)
	}
	return resources, nil
}
