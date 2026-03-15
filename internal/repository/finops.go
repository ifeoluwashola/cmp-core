// internal/repository/finops.go
// Repository methods for the daily_costs table (FinOps Engine).
//
// Queries run inside a WithOrgTx transaction so RLS automatically constrains
// results to the calling organization. No explicit tenant filter is required.

package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CostSummaryRow holds aggregated spend for a single service category.
type CostSummaryRow struct {
	ServiceCategory string `json:"service_category"`
	// TotalAmount is a string-encoded NUMERIC to avoid float64 rounding loss.
	TotalAmount string `json:"total_amount"`
	Currency    string `json:"currency"`
}

// FinOpsRepository is a thin SQL wrapper for FinOps-related queries.
type FinOpsRepository struct{}

// NewFinOpsRepository constructs a FinOpsRepository.
func NewFinOpsRepository() *FinOpsRepository { return &FinOpsRepository{} }

// GetCostSummary returns total spend grouped by service_category for the
// current calendar month. Results are ordered from highest to lowest cost.
func (r *FinOpsRepository) GetCostSummary(ctx context.Context, tx pgx.Tx) ([]CostSummaryRow, error) {
	const q = `
		SELECT
			service_category,
			SUM(amount)::TEXT  AS total_amount,
			MAX(currency)      AS currency
		FROM daily_costs
		WHERE date >= date_trunc('month', NOW())
		  AND date <  date_trunc('month', NOW()) + INTERVAL '1 month'
		GROUP BY service_category
		ORDER BY SUM(amount) DESC`

	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("repository: cost summary: %w", err)
	}
	defer rows.Close()

	var summary []CostSummaryRow
	for rows.Next() {
		var row CostSummaryRow
		if err := rows.Scan(&row.ServiceCategory, &row.TotalAmount, &row.Currency); err != nil {
			return nil, fmt.Errorf("repository: scan cost summary: %w", err)
		}
		summary = append(summary, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate cost summary: %w", err)
	}
	return summary, nil
}
