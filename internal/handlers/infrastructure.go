// internal/handlers/infrastructure.go
// HTTP handlers for GET /api/v1/infrastructure.
//
// Returns all infrastructure resources belonging to the caller's organization.
// An optional ?env_id= query parameter narrows results to a single environment.

package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/database"
	"github.com/ifeoluwashola/cmp-core/internal/middleware"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/ifeoluwashola/cmp-core/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InfrastructureHandler serves requests for infrastructure resource data.
type InfrastructureHandler struct {
	pool *pgxpool.Pool
	repo *repository.InfrastructureRepository
}

// NewInfrastructureHandler constructs the handler.
func NewInfrastructureHandler(pool *pgxpool.Pool) *InfrastructureHandler {
	return &InfrastructureHandler{
		pool: pool,
		repo: repository.NewInfrastructureRepository(),
	}
}

// List handles GET /api/v1/infrastructure.
//
//	@Summary     List infrastructure resources for the tenant
//	@Description Returns all discovered cloud resources. Filter by env_id to scope to one environment.
//	@Tags        infrastructure
//	@Produce     json
//	@Security    BearerAuth
//	@Param       env_id  query     string  false  "Filter by cloud environment UUID"
//	@Success     200     {array}   models.InfrastructureResource
//	@Failure     400     {object}  map[string]string
//	@Failure     401     {object}  map[string]string
//	@Failure     500     {object}  map[string]string
//	@Router      /api/v1/infrastructure [get]
func (h *InfrastructureHandler) List(c *gin.Context) {
	orgID, ok := middleware.OrgIDFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "organization context missing"})
		return
	}

	// Optional environment filter.
	var envID *uuid.UUID
	if raw := c.Query("env_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid env_id: must be a UUID"})
			return
		}
		envID = &parsed
	}

	var resources []*models.InfrastructureResource
	err := database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		var txErr error
		resources, txErr = h.repo.ListResources(c.Request.Context(), tx, envID)
		return txErr
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list resources: " + err.Error()})
		return
	}

	// Return an empty array (not null) when there are no records.
	if resources == nil {
		resources = []*models.InfrastructureResource{}
	}
	c.JSON(http.StatusOK, resources)
}
