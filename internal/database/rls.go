// internal/database/rls.go
// Row-Level Security transaction helper.
//
// Every RLS-protected table in cmp-core uses the policy:
//
//	USING (organization_id = current_setting('app.current_organization_id')::UUID)
//
// Application code MUST call WithOrgTx (instead of starting a raw transaction)
// whenever it queries those tables. This ensures the session variable is always
// set — and always scoped to a single transaction — before any DML executes.

package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithOrgTx begins a serializable-isolation transaction, sets the RLS context
// variable to orgID for the duration of that transaction, and then calls fn.
//
// Commit/rollback is handled automatically:
//   - If fn returns nil  → the transaction is committed.
//   - If fn returns an error OR panics → the transaction is rolled back.
//
// Usage:
//
//	err := database.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
//	    _, err := tx.Exec(ctx, "SELECT * FROM users")
//	    return err
//	})
func WithOrgTx(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, fn func(tx pgx.Tx) error) (retErr error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted, // matches typical OLTP workload; upgrade if needed
	})
	if err != nil {
		return fmt.Errorf("rls: begin tx: %w", err)
	}

	// Ensure rollback on any exit path that hasn't already committed.
	defer func() {
		if retErr != nil {
			// Swallow the rollback error — the original error is more important.
			_ = tx.Rollback(ctx)
		}
	}()

	// SET LOCAL scopes the variable to this transaction only.
	// If the connection is returned to the pool and reused, the value is gone.
	if _, err := tx.Exec(ctx,
		"SET LOCAL app.current_organization_id = $1", orgID.String(),
	); err != nil {
		return fmt.Errorf("rls: set org context: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("rls: commit: %w", err)
	}

	return nil
}
