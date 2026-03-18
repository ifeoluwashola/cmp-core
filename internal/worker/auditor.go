// internal/worker/auditor.go
// Background auditing worker.
//
// On every tick the Auditor:
//  1. Fetches all *active* cloud environments across all tenants (using
//     WithServiceTx / cmp_service role to bypass RLS).
//  2. For each environment, calls the registered cloud Provider to get
//     the current list of infrastructure resources AND daily costs.
//  3. Upserts resources into infrastructure_resources and costs into
//     daily_costs under the correct tenant context (WithOrgTx / RLS).

package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
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

// StartResourceAuditor runs the resource audit loop until ctx is cancelled.
// Call it in a goroutine; it blocks until the context is done.
func (a *Auditor) StartResourceAuditor(ctx context.Context, ticker *time.Ticker) {
	log.Println("auditor: resource auditor started, waiting for first tick…")
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

// auditEnvironment polls one cloud environment and upserts its resources and costs.
// Returns the number of resources upserted.
func (a *Auditor) auditEnvironment(ctx context.Context, env models.CloudEnvironment) (int, error) {
	// ── Step 2: Fetch current resources from the cloud provider ────────────
	provider, err := a.registry.Get(env.Provider)
	if err != nil {
		return 0, fmt.Errorf("auditEnvironment: %w", err)
	}

	resources, err := provider.FetchResources(ctx, env)
	if err != nil {
		return 0, fmt.Errorf("auditEnvironment: fetch resources: %w", err)
	}

	if len(resources) == 0 {
		return 0, nil
	}

	// ── Step 3: Upsert resources under the correct tenant context ─────────
	scanTime := time.Now().UTC()
	err = database.WithOrgTx(ctx, a.pool, env.OrganizationID, func(tx pgx.Tx) error {
		// Update resources to have the current scanTime
		for i := range resources {
			resources[i].LastAuditedAt = &scanTime
		}

		if err := upsertResources(ctx, tx, resources); err != nil {
			return err
		}

		// Reconciliation: Mark resources NOT seen in this scan as deleted
		return markMissingResourcesDeleted(ctx, tx, env.ID, scanTime)
	})
	if err != nil {
		return 0, fmt.Errorf("auditEnvironment: sync: %w", err)
	}

	return len(resources), nil
}

// StartCostAuditor runs the cost audit loop until ctx is cancelled.
func (a *Auditor) StartCostAuditor(ctx context.Context, ticker *time.Ticker) {
	log.Println("auditor: cost auditor started, waiting for first tick…")
	for {
		select {
		case <-ctx.Done():
			log.Println("auditor: cost auditor context cancelled, shutting down")
			return
		case t := <-ticker.C:
			log.Printf("auditor: cost tick at %s — starting cost cycle", t.Format(time.RFC3339))
			if err := a.RunCostCycle(ctx); err != nil {
				log.Printf("auditor: cost cycle error: %v", err)
			}
		}
	}
}

// RunCostCycle performs a single full cost pass. Exposed so main.go can manually trigger it immediately.
func (a *Auditor) RunCostCycle(ctx context.Context) error {
	var envs []models.CloudEnvironment
	err := database.WithServiceTx(ctx, a.pool, func(tx pgx.Tx) error {
		var txErr error
		envs, txErr = fetchActiveEnvironments(ctx, tx)
		return txErr
	})
	if err != nil {
		return fmt.Errorf("RunCostCycle: list environments: %w", err)
	}

	log.Printf("auditor: found %d active environment(s) for cost sync", len(envs))

	for _, env := range envs {
		if err := a.auditCostEnvironment(ctx, env); err != nil {
			log.Printf("auditor: cost environment %s (%s): %v", env.Name, env.ID, err)
			continue
		}
	}

	return nil
}

// auditCostEnvironment polls one cloud environment and upserts its costs.
func (a *Auditor) auditCostEnvironment(ctx context.Context, env models.CloudEnvironment) error {
	provider, err := a.registry.Get(env.Provider)
	if err != nil {
		return fmt.Errorf("auditCostEnvironment: %w", err)
	}

	costs, err := provider.FetchCosts(ctx, env)
	if err != nil {
		return fmt.Errorf("auditCostEnvironment: fetch costs: %w", err)
	}

	if len(costs) == 0 {
		return nil
	}

	err = database.WithOrgTx(ctx, a.pool, env.OrganizationID, func(tx pgx.Tx) error {
		return upsertCosts(ctx, tx, costs)
	})
	if err != nil {
		return fmt.Errorf("auditCostEnvironment: upsert: %w", err)
	}

	return nil
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
			r.Attributes,
			r.Status,
			r.LastAuditedAt,
		); err != nil {
			return fmt.Errorf("upsertResources %s: %w", r.ProviderResourceID, err)
		}
	}
	return nil
}

func markMissingResourcesDeleted(ctx context.Context, tx pgx.Tx, envID uuid.UUID, scanTime time.Time) error {
	// Any resource belonging to this environment that wasn't updated by the current scan
	// (i.e. last_audited_at < scanTime) and isn't already 'deleted' should be marked as such.
	const q = `
		UPDATE infrastructure_resources
		SET    status = 'deleted',
		       updated_at = NOW()
		WHERE  environment_id = $1
		  AND  last_audited_at < $2
		  AND  status != 'deleted'`

	tag, err := tx.Exec(ctx, q, envID, scanTime)
	if err != nil {
		return fmt.Errorf("markMissingResourcesDeleted: %w", err)
	}

	if rows := tag.RowsAffected(); rows > 0 {
		log.Printf("auditor: reconciled environment %s — marked %d resource(s) as deleted", envID, rows)
	}

	return nil
}

func upsertCosts(ctx context.Context, tx pgx.Tx, costs []models.DailyCost) error {
	// ON CONFLICT target must match the constraint defined in migration 000006:
	//   UNIQUE (environment_id, date, service_category)
	// organization_id is intentionally omitted — environment_id already implies it,
	// and RLS prevents cross-org access at the session level.
	const q = `
		INSERT INTO daily_costs
			(organization_id, environment_id, date, service_category, amount, currency)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (environment_id, date, service_category) DO UPDATE SET
			amount   = EXCLUDED.amount,
			currency = EXCLUDED.currency`

	for _, c := range costs {
		if _, err := tx.Exec(ctx, q,
			c.OrganizationID,
			c.EnvironmentID,
			c.Date,
			c.ServiceCategory,
			c.Amount,
			c.Currency,
		); err != nil {
			return fmt.Errorf("upsertCosts %s/%s: %w", c.Date.Format("2006-01-02"), c.ServiceCategory, err)
		}
	}
	return nil
}
