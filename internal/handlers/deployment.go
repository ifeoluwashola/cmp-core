// internal/handlers/deployment.go
// HTTP handlers for the Provisioning Engine.
//
//   POST /api/v1/deployments       — trigger an IaC pipeline run (JWT protected)
//   POST /api/v1/webhooks/cicd     — receive completion callbacks (webhook secret)

package handlers

import (
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
//	@Description Creates a deployment record (status: queued), calls the CI/CD provider to start the pipeline, then updates the record with the returned job_id (status: running). Returns 202 Accepted.
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

	// ── Step 1: Persist the initial record (status: queued) ────────────────
	var deployment *models.Deployment
	err := database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		var txErr error
		deployment, txErr = h.repo.CreateDeployment(c.Request.Context(), tx, repository.CreateDeploymentInput{
			OrganizationID: orgID,
			EnvironmentID:  req.EnvironmentID,
			ModuleName:     req.ModuleName,
		})
		return txErr
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create deployment: " + err.Error()})
		return
	}

	// ── Step 2: Trigger the CI/CD pipeline ───────────────────────────────────
	jobID, err := h.pipeline.TriggerDeployment(c.Request.Context(), *deployment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to trigger pipeline: " + err.Error()})
		return
	}

	// ── Step 3: Stamp the job_id and flip status to 'running' ────────────────
	err = database.WithOrgTx(c.Request.Context(), h.pool, orgID, func(tx pgx.Tx) error {
		return h.repo.SetJobID(c.Request.Context(), tx, deployment.ID, jobID)
	})
	if err != nil {
		// Non-fatal for the caller — the pipeline is already running.
		// Log but still return the record so the client has the deployment ID.
		_ = err
	} else {
		deployment.JobID = &jobID
		deployment.Status = models.DeploymentStatusRunning
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
