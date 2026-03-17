// internal/handlers/cloud_env.go
// HTTP handlers for the /api/v1/environments resource.
//
// Flow per request:
//  1. TenantMiddleware has already validated X-Organization-ID → orgID in context.
//  2. Handler opens an RLS-scoped transaction via database.WithOrgTx.
//  3. Inside that transaction, it calls the repository.
//  4. On success it returns 200/201 JSON; on failure 4xx/5xx.

package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ifeoluwashola/cmp-core/internal/database"
	"github.com/ifeoluwashola/cmp-core/internal/middleware"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/ifeoluwashola/cmp-core/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CloudEnvHandler groups the HTTP handlers for cloud environments.
type CloudEnvHandler struct {
	pool *pgxpool.Pool
	repo *repository.CloudEnvRepository
}

// NewCloudEnvHandler constructs the handler with its dependencies.
func NewCloudEnvHandler(pool *pgxpool.Pool) *CloudEnvHandler {
	return &CloudEnvHandler{
		pool: pool,
		repo: repository.NewCloudEnvRepository(),
	}
}

// ─── POST /api/v1/environments ────────────────────────────────────────────────

// createRequest is the JSON body for registering a new cloud environment.
type createCloudEnvRequest struct {
	Name     string               `json:"name"      binding:"required"`
	Provider models.CloudProvider `json:"provider"  binding:"required,oneof=aws gcp azure other"`
	AuthType            models.AuthType      `json:"auth_type" binding:"required,oneof=oidc iam_cross_account_role service_account_key"`
	RoleARN             *string              `json:"role_arn"`
	ProvisioningRoleARN *string              `json:"provisioning_role_arn"`
	// Regions lists the cloud regions to audit. Leave empty to use the provider default.
	Regions             []string             `json:"regions"`
}

// Create handles POST /api/v1/environments.
//
//	@Summary     Register a new cloud environment (spoke)
//	@Tags        environments
//	@Accept      json
//	@Produce     json
//	@Security    BearerAuth
//	@Param       body  body      createCloudEnvRequest  true  "Environment details"
//	@Success     201   {object}  models.CloudEnvironment
//	@Failure     400   {object}  map[string]string
//	@Failure     401   {object}  map[string]string
//	@Failure     500   {object}  map[string]string
//	@Router      /api/v1/environments [post]
func (h *CloudEnvHandler) Create(c *gin.Context) {
	orgID, ok := middleware.OrgIDFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "organization context missing"})
		return
	}

	var req createCloudEnvRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var created *models.CloudEnvironment
	err := database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		var txErr error
		created, txErr = h.repo.Create(c.Request.Context(), tx, repository.CreateCloudEnvInput{
			OrganizationID:      orgID,
			Name:                req.Name,
			Provider:            req.Provider,
			AuthType:            req.AuthType,
			RoleARN:             req.RoleARN,
			ProvisioningRoleARN: req.ProvisioningRoleARN,
			Regions:             req.Regions, // nil → provider default applied in repository
		})
		return txErr
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create environment: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, created)
}

// ─── GET /api/v1/environments ─────────────────────────────────────────────────

// List handles GET /api/v1/environments.
//
//	@Summary  List all cloud environments for the tenant
//	@Tags     environments
//	@Produce  json
//	@Security BearerAuth
//	@Success  200  {array}   models.CloudEnvironment
//	@Failure  401  {object}  map[string]string
//	@Failure  500  {object}  map[string]string
//	@Router   /api/v1/environments [get]
func (h *CloudEnvHandler) List(c *gin.Context) {
	orgID, ok := middleware.OrgIDFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "organization context missing"})
		return
	}

	var envs []*models.CloudEnvironment
	err := database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		var txErr error
		envs, txErr = h.repo.List(c.Request.Context(), tx)
		return txErr
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list environments: " + err.Error()})
		return
	}

	// Return an empty array (not null) when there are no records.
	if envs == nil {
		envs = []*models.CloudEnvironment{}
	}
	c.JSON(http.StatusOK, envs)
}
