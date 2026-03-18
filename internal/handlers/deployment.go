// internal/handlers/deployment.go
// HTTP handlers for the Provisioning Engine.
//
//	POST /api/v1/deployments       — trigger an IaC pipeline run (JWT protected)
//	GET  /api/v1/deployments       — list deployment history (JWT protected)
//	GET  /api/v1/deployments/:id   — fetch single deployment / poll status (JWT protected)
//	POST /api/v1/webhooks/cicd     — receive completion callbacks (webhook secret)

package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/cicd"
	"github.com/ifeoluwashola/cmp-core/internal/database"
	"github.com/ifeoluwashola/cmp-core/internal/middleware"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/ifeoluwashola/cmp-core/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeploymentHandler wires the CI/CD provider and repository for deployment operations.
type DeploymentHandler struct {
	pool     *pgxpool.Pool
	repo     *repository.DeploymentRepository
	pipeline cicd.PipelineProvider
}

// NewDeploymentHandler constructs the handler with its dependencies.
func NewDeploymentHandler(pool *pgxpool.Pool, pipeline cicd.PipelineProvider) *DeploymentHandler {
	return &DeploymentHandler{
		pool:     pool,
		repo:     repository.NewDeploymentRepository(),
		pipeline: pipeline,
	}
}

// ─── POST /api/v1/deployments ─────────────────────────────────────────────────

// triggerRequest is the JSON body for triggering a new deployment.
type triggerRequest struct {
	EnvironmentID uuid.UUID `json:"environment_id" binding:"required"`
	ModuleName    string    `json:"module_name"    binding:"required"`
}

// TriggerDeployment handles POST /api/v1/deployments.
//
//	@Summary     Trigger an IaC pipeline deployment
//	@Description Creates a deployment record (status: queued), calls the GitHub action workflow, and sets the status dynamically based on HTTP request results.
//	@Tags        deployments
//	@Accept      json
//	@Produce     json
//	@Security    BearerAuth
//	@Param       body  body      triggerRequest    true  "Deployment details"
//	@Success     202   {object}  models.Deployment
//	@Failure     400   {object}  map[string]string
//	@Failure     401   {object}  map[string]string
//	@Failure     500   {object}  map[string]string
//	@Router      /api/v1/deployments [post]
func (h *DeploymentHandler) TriggerDeployment(c *gin.Context) {
	orgID, ok := middleware.OrgIDFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "organization context missing"})
		return
	}

	var req triggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ── Step 1: Initialize external API credentials
	githubClient, err := cicd.NewGitHubClient()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to initialize ci/cd provider: " + err.Error()})
		return
	}

	// ── Step 2: Grab the Environment `RoleArn` for CI/CD integrations scoped to Org rules.
	// var envRoleARN string
	var envProvisioningRoleARN string
	var deployment *models.Deployment

	err = database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		// Use inline query directly against environments as we don't have GetEnvironmentByID in repo structs yet
		const q = `SELECT role_arn, provisioning_role_arn FROM cloud_environments WHERE id = $1 LIMIT 1`
		var rn, prn *string
		if e := tx.QueryRow(c.Request.Context(), q, req.EnvironmentID).Scan(&rn, &prn); e != nil {
			return fmt.Errorf("failed fetching environment details: %w", e)
		}
		
		// Fallback onto Audit Role (RoleARN) natively if Provisioning Role isn't strictly defined by the organization.
		if prn != nil && *prn != "" {
			envProvisioningRoleARN = *prn
		} else if rn != nil {
			envProvisioningRoleARN = *rn
		}

		// Create deployment sequentially.
		var txErr error
		deployment, txErr = h.repo.CreateDeployment(c.Request.Context(), tx, repository.CreateDeploymentInput{
			OrganizationID: orgID,
			EnvironmentID:  req.EnvironmentID,
			ModuleName:     req.ModuleName,
		})
		return txErr
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// ── Step 3: Trigger the pipeline
	err = githubClient.TriggerWorkflow(c.Request.Context(), deployment.ID, deployment.ModuleName, deployment.EnvironmentID, envProvisioningRoleARN)
	if err != nil {
		// ── Step 4a: Roll state back to 'failed' since dispatch failed gracefully
		_ = database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
			return h.repo.UpdateDeploymentStatus(c.Request.Context(), tx, deployment.ID, models.DeploymentStatusFailed, "GitHub Actions dispatch rejected: "+err.Error())
		})
		deployment.Status = models.DeploymentStatusFailed
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed triggering cloud provision: " + err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, deployment)
}

// ─── POST /api/v1/webhooks/cicd ───────────────────────────────────────────────

// webhookRequest is the JSON body sent by the CI/CD pipeline on completion.
type webhookRequest struct {
	JobID  string `json:"job_id"  binding:"required"`
	Status string `json:"status"  binding:"required,oneof=success failed"`
	Logs   string `json:"logs"`
}

// WebhookCallback handles POST /api/v1/webhooks/cicd.
//
//	@Summary     Receive CI/CD pipeline completion callbacks
//	@Description Called by the CI/CD system when a pipeline finishes. Authenticated by X-Webhook-Secret header (not a user JWT). Updates the deployment record status and logs.
//	@Tags        webhooks
//	@Accept      json
//	@Produce     json
//	@Param       X-Webhook-Secret  header    string          true  "Pre-shared webhook secret"
//	@Param       body              body      webhookRequest  true  "Pipeline result"
//	@Success     200               {object}  map[string]string
//	@Failure     400               {object}  map[string]string
//	@Failure     401               {object}  map[string]string
//	@Failure     404               {object}  map[string]string
//	@Failure     500               {object}  map[string]string
//	@Router      /api/v1/webhooks/cicd [post]
func (h *DeploymentHandler) WebhookCallback(c *gin.Context) {
	var req webhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ── Step 1: Resolve deployment by job_id (service-role bypasses RLS) ─────
	var deployment *models.Deployment
	err := database.WithServiceTx(c.Request.Context(), h.pool, func(tx pgx.Tx) error {
		var txErr error
		deployment, txErr = h.repo.GetDeploymentByJobID(c.Request.Context(), tx, req.JobID)
		return txErr
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found for job_id: " + req.JobID})
		return
	}

	// ── Step 2: Update status + logs scoped to the correct org (RLS enforced) ─
	newStatus := models.DeploymentStatus(req.Status)
	err = database.WithOrgTx(c.Request.Context(), h.pool, deployment.OrganizationID, func(tx pgx.Tx) error {
		return h.repo.UpdateDeploymentStatus(c.Request.Context(), tx, deployment.ID, newStatus, req.Logs)
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update deployment: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "deployment updated", "job_id": req.JobID, "status": req.Status})
}

// ─── GET /api/v1/deployments ──────────────────────────────────────────────────

// ListDeployments handles GET /api/v1/deployments.
//
//	@Summary     List deployment history
//	@Description Returns all deployments for the tenant, newest first. Filter by env_id to scope to one environment.
//	@Tags        deployments
//	@Produce     json
//	@Security    BearerAuth
//	@Param       env_id  query     string  false  "Filter by cloud environment UUID"
//	@Success     200     {array}   models.Deployment
//	@Failure     400     {object}  map[string]string
//	@Failure     401     {object}  map[string]string
//	@Failure     500     {object}  map[string]string
//	@Router      /api/v1/deployments [get]
func (h *DeploymentHandler) ListDeployments(c *gin.Context) {
	orgID, ok := middleware.OrgIDFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "organization context missing"})
		return
	}

	var envID *uuid.UUID
	if raw := c.Query("env_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid env_id: must be a UUID"})
			return
		}
		envID = &parsed
	}

	var deployments []*models.Deployment
	err := database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		var txErr error
		deployments, txErr = h.repo.ListDeployments(c.Request.Context(), tx, envID)
		return txErr
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deployments: " + err.Error()})
		return
	}

	if deployments == nil {
		deployments = []*models.Deployment{}
	}
	c.JSON(http.StatusOK, deployments)
}

// ─── GET /api/v1/deployments/:id ──────────────────────────────────────────────

// GetDeployment handles GET /api/v1/deployments/:id.
//
//	@Summary     Get a single deployment
//	@Description Fetches a deployment by its UUID — useful for polling status or reading logs after completion.
//	@Tags        deployments
//	@Produce     json
//	@Security    BearerAuth
//	@Param       id   path      string  true  "Deployment UUID"
//	@Success     200  {object}  models.Deployment
//	@Failure     400  {object}  map[string]string
//	@Failure     401  {object}  map[string]string
//	@Failure     404  {object}  map[string]string
//	@Failure     500  {object}  map[string]string
//	@Router      /api/v1/deployments/{id} [get]
func (h *DeploymentHandler) GetDeployment(c *gin.Context) {
	orgID, ok := middleware.OrgIDFromContext(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "organization context missing"})
		return
	}

	deploymentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id: must be a UUID"})
		return
	}

	var deployment *models.Deployment
	err = database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		var txErr error
		deployment, txErr = h.repo.GetDeploymentByID(c.Request.Context(), tx, deploymentID)
		return txErr
	})
	if err != nil {
		if err.Error() == "no rows in result set" {
			c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get deployment: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, deployment)
}
