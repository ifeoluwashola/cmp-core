// internal/handlers/finops.go
// HTTP handlers for FinOps (cost) endpoints.
//
//	GET /api/v1/costs/summary — current-month spend grouped by service category.

package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ifeoluwashola/cmp-core/internal/database"
	"github.com/ifeoluwashola/cmp-core/internal/middleware"
	"github.com/ifeoluwashola/cmp-core/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FinOpsHandler serves FinOps cost analytics requests.
type FinOpsHandler struct {
	pool *pgxpool.Pool
	repo *repository.FinOpsRepository
}

// NewFinOpsHandler constructs the handler.
func NewFinOpsHandler(pool *pgxpool.Pool) *FinOpsHandler {
	return &FinOpsHandler{
		pool: pool,
		repo: repository.NewFinOpsRepository(),
	}
}

// GetCostSummary handles GET /api/v1/costs/summary.
//
//	@Summary     Current-month cost summary by service category
//	@Description Aggregates daily_costs for the current calendar month, grouped by service category.
//	@Tags        finops
//	@Produce     json
//	@Security    BearerAuth
//	@Success     200  {array}   repository.CostSummaryRow
//	@Failure     401  {object}  map[string]string
//	@Failure     500  {object}  map[string]string
//	@Router      /api/v1/costs/summary [get]
func (h *FinOpsHandler) GetCostSummary(c *gin.Context) {
	orgID, ok := middleware.OrgIDFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "organization context missing"})
		return
	}

	var summary []repository.CostSummaryRow
	err := database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		var txErr error
		summary, txErr = h.repo.GetCostSummary(c.Request.Context(), tx)
		return txErr
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get cost summary: " + err.Error()})
		return
	}

	// Return an empty array (not null) when there is no data yet.
	if summary == nil {
		summary = []repository.CostSummaryRow{}
	}
	c.JSON(http.StatusOK, summary)
}
