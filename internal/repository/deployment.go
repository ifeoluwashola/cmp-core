// internal/repository/deployment.go
// Repository methods for the deployments table (Provisioning Engine).
//
// All write methods accept a *pgx.Tx from database.WithOrgTx, so RLS scopes
// every mutation to the calling organization automatically.
//
// GetDeploymentByJobID is the exception: it is called inside a WithServiceTx
// by the webhook handler, which has no user JWT. The service-role bypass policy
// lets it resolve any deployment cross-org so the handler can extract org_id
// and then open a WithOrgTx for the subsequent status update.

package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/jackc/pgx/v5"
)

// DeploymentRepository is a thin SQL wrapper for the deployments table.
type DeploymentRepository struct{}

// NewDeploymentRepository constructs a DeploymentRepository.
func NewDeploymentRepository() *DeploymentRepository { return &DeploymentRepository{} }

// ─── Inputs ───────────────────────────────────────────────────────────────────

// CreateDeploymentInput carries the fields required to start a new deployment.
type CreateDeploymentInput struct {
	OrganizationID uuid.UUID
	EnvironmentID  uuid.UUID
	ModuleName     string
}

// ─── Methods ──────────────────────────────────────────────────────────────────

// CreateDeployment inserts a new deployment record with status 'queued' and
// returns the fully populated row. Must be called inside a WithOrgTx.
func (r *DeploymentRepository) CreateDeployment(
	ctx context.Context,
	tx pgx.Tx,
	in CreateDeploymentInput,
) (*models.Deployment, error) {
	const q = `
		INSERT INTO deployments
			(organization_id, environment_id, module_name, status)
		VALUES ($1, $2, $3, 'queued')
		RETURNING
			id, organization_id, environment_id, module_name,
			status, job_id, logs, created_at, updated_at`

	row := tx.QueryRow(ctx, q, in.OrganizationID, in.EnvironmentID, in.ModuleName)
	d, err := scanDeployment(row)
	if err != nil {
		return nil, fmt.Errorf("repository: create deployment: %w", err)
	}
	return d, nil
}

// GetDeploymentByJobID looks up a deployment by its CI/CD job ID.
// Intended for use inside a WithServiceTx (cmp_service bypasses RLS) so the
// webhook handler can resolve the org without a user JWT.
func (r *DeploymentRepository) GetDeploymentByJobID(
	ctx context.Context,
	tx pgx.Tx,
	jobID string,
) (*models.Deployment, error) {
	const q = `
		SELECT id, organization_id, environment_id, module_name,
		       status, job_id, logs, created_at, updated_at
		FROM deployments
		WHERE job_id = $1
		LIMIT 1`

	row := tx.QueryRow(ctx, q, jobID)
	d, err := scanDeployment(row)
	if err != nil {
		return nil, fmt.Errorf("repository: get deployment by job_id %q: %w", jobID, err)
	}
	return d, nil
}

// SetJobID updates the job_id and flips the status to 'running' right after the
// CI/CD provider confirms the pipeline was triggered. Must run inside a WithOrgTx.
func (r *DeploymentRepository) SetJobID(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	jobID string,
) error {
	const q = `
		UPDATE deployments
		SET job_id = $1, status = 'running', updated_at = NOW()
		WHERE id = $2`

	if _, err := tx.Exec(ctx, q, jobID, id); err != nil {
		return fmt.Errorf("repository: set job_id for deployment %s: %w", id, err)
	}
	return nil
}

// UpdateDeploymentStatus sets status and logs for a deployment as reported by
// the CI/CD webhook. Must run inside a WithOrgTx scoped to the correct org.
func (r *DeploymentRepository) UpdateDeploymentStatus(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	status models.DeploymentStatus,
	logs string,
) error {
	const q = `
		UPDATE deployments
		SET status = $1, logs = $2, updated_at = $3
		WHERE id = $4`

	if _, err := tx.Exec(ctx, q, string(status), logs, time.Now().UTC(), id); err != nil {
		return fmt.Errorf("repository: update deployment status %s: %w", id, err)
	}
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func scanDeployment(s pgxScanner) (*models.Deployment, error) {
	var (
		d      models.Deployment
		status string
	)
	err := s.Scan(
		&d.ID,
		&d.OrganizationID,
		&d.EnvironmentID,
		&d.ModuleName,
		&status,
		&d.JobID,
		&d.Logs,
		&d.CreatedAt,
		&d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	d.Status = models.DeploymentStatus(status)
	return &d, nil
}
