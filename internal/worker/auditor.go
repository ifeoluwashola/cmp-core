// internal/worker/auditor.go
// Background auditing worker.
//
// On every tick the Auditor:
//  1. Fetches all *active* cloud environments across all tenants (using
//     WithServiceTx / cmp_service role to bypass RLS).
//  2. For each environment, calls the registered cloud Provider to get
//     the current list of infrastructure resources.
//  3. Upserts those resources into the database under the correct tenant
//     context using WithOrgTx (which enforces RLS for the write path).

package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ifeoluwashola/cmp-core/internal/cloud"
	"github.com/ifeoluwashola/cmp-core/internal/database"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Auditor polls cloud providers and keeps infrastructure_resources up-to-date.
type Auditor struct {
	pool     *pgxpool.Pool
	registry cloud.Registry
}

// NewAuditor constructs an Auditor.
func NewAuditor(pool *pgxpool.Pool, registry cloud.Registry) *Auditor {
	return &Auditor{pool: pool, registry: registry}
}

// Start runs the audit loop until ctx is cancelled.
// Call it in a goroutine; it blocks until the context is done.
func (a *Auditor) Start(ctx context.Context, ticker *time.Ticker) {
	log.Println("auditor: started, waiting for first tick…")
	for {
		select {
		case <-ctx.Done():
			log.Println("auditor: context cancelled, shutting down")
			return
		case t := <-ticker.C:
			log.Printf("auditor: tick at %s — starting audit cycle", t.Format(time.RFC3339))
			if err := a.runCycle(ctx); err != nil {
				log.Printf("auditor: cycle error: %v", err)
			}
		}
	}
}

// runCycle performs a single full audit pass.
func (a *Auditor) runCycle(ctx context.Context) error {
	// ── Step 1: Fetch all active environments (cross-org, service bypass) ────
	var envs []models.CloudEnvironment
	err := database.WithServiceTx(ctx, a.pool, func(tx pgx.Tx) error {
		var txErr error
		envs, txErr = fetchActiveEnvironments(ctx, tx)
		return txErr
	})
	if err != nil {
		return fmt.Errorf("runCycle: list environments: %w", err)
	}

	log.Printf("auditor: found %d active environment(s)", len(envs))

	total := 0
	for _, env := range envs {
		n, err := a.auditEnvironment(ctx, env)
		if err != nil {
			// Log and continue — one bad environment shouldn't abort all others.
			log.Printf("auditor: environment %s (%s): %v", env.Name, env.ID, err)
			continue
		}
		total += n
	}

	log.Printf("auditor: cycle complete — upserted %d resource(s)", total)
	return nil
}

// auditEnvironment polls one cloud environment and upserts its resources.
// Returns the number of resources upserted.
func (a *Auditor) auditEnvironment(ctx context.Context, env models.CloudEnvironment) (int, error) {
	// ── Step 2: Fetch current resources from the cloud provider ────────────
	provider, err := a.registry.Get(env.Provider)
	if err != nil {
		return 0, fmt.Errorf("auditEnvironment: %w", err)
	}

	resources, err := provider.FetchResources(ctx, env)
	if err != nil {
		return 0, fmt.Errorf("auditEnvironment: fetch: %w", err)
	}

	if len(resources) == 0 {
		return 0, nil
	}

	// ── Step 3: Upsert resources under the correct tenant context ──────────
	err = database.WithOrgTx(ctx, a.pool, env.OrganizationID, func(tx pgx.Tx) error {
		return upsertResources(ctx, tx, resources)
	})
	if err != nil {
		return 0, fmt.Errorf("auditEnvironment: upsert: %w", err)
	}

	return len(resources), nil
}

// ─── SQL helpers ──────────────────────────────────────────────────────────────

func fetchActiveEnvironments(ctx context.Context, tx pgx.Tx) ([]models.CloudEnvironment, error) {
	const q = `
		SELECT id, organization_id, name, provider, auth_type,
		       role_arn, connection_status, created_at, updated_at
		FROM   cloud_environments
		WHERE  connection_status = 'active'
		ORDER  BY organization_id, created_at`

	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("fetchActiveEnvironments: %w", err)
	}
	defer rows.Close()

	var envs []models.CloudEnvironment
	for rows.Next() {
		var (
			env      models.CloudEnvironment
			provider string
			authType string
			status   string
		)
		if err := rows.Scan(
			&env.ID, &env.OrganizationID, &env.Name,
			&provider, &authType, &env.RoleARN,
			&status, &env.CreatedAt, &env.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("fetchActiveEnvironments scan: %w", err)
		}
		env.Provider = models.CloudProvider(provider)
		env.AuthType = models.AuthType(authType)
		env.ConnectionStatus = models.ConnStatus(status)
		envs = append(envs, env)
	}
	return envs, rows.Err()
}

func upsertResources(ctx context.Context, tx pgx.Tx, resources []models.InfrastructureResource) error {
	const q = `
		INSERT INTO infrastructure_resources
			(organization_id, environment_id, provider_resource_id,
			 resource_type, attributes, status, last_audited_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (environment_id, provider_resource_id) DO UPDATE SET
			resource_type   = EXCLUDED.resource_type,
			attributes      = EXCLUDED.attributes,
			status          = EXCLUDED.status,
			last_audited_at = EXCLUDED.last_audited_at,
			updated_at      = NOW()`

	for _, r := range resources {
		if _, err := tx.Exec(ctx, q,
			r.OrganizationID,
			r.EnvironmentID,
			r.ProviderResourceID,
			r.ResourceType,
			r.Attributes, // json.RawMessage → pgx sends as JSONB
			r.Status,
			r.LastAuditedAt,
		); err != nil {
			return fmt.Errorf("upsertResources %s: %w", r.ProviderResourceID, err)
		}
	}
	return nil
}
