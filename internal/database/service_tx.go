// internal/database/service_tx.go
// Cross-tenant transaction helper for privileged internal services.
//
// The cmp_service role has a USING (true) bypass policy on every RLS-protected
// table, meaning it can see rows from all organizations. Use WithServiceTx when
// a query legitimately needs cross-org visibility (e.g., the auditing service
// fetching all active cloud environments to poll).
//
// Prerequisite in PostgreSQL:
//
//	GRANT cmp_service TO <your_login_role>;
//
// (Added to scripts/db-setup.sh automatically.)

package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithServiceTx begins a transaction and elevates the session role to
// cmp_service for the duration of that transaction. This grants full
// cross-tenant visibility via the service bypass RLS policies.
//
// Commit/rollback semantics are identical to WithOrgTx:
//
//   - fn returns nil  → commit
//   - fn returns error → rollback
func WithServiceTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (retErr error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("service_tx: begin: %w", err)
	}

	defer func() {
		if retErr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Switch to the cmp_service role so the bypass policy applies.
	// SET LOCAL ROLE is scoped to this transaction only.
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE cmp_service"); err != nil {
		return fmt.Errorf("service_tx: set role: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("service_tx: commit: %w", err)
	}
	return nil
}
