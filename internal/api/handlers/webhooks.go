package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// WebhookPayload represents the standard JSON data transmitted from GitHub Actions.
type WebhookPayload struct {
	DeploymentID string `json:"deployment_id" binding:"required,uuid"`
	Status       string `json:"status"        binding:"required,oneof=success failed canceled"`
	JobID        string `json:"job_id"        binding:"required"`
	Logs         string `json:"logs"`
}

// HandleDeploymentWebhook receives HTTP POST traffic from the external CI/CD engine signaling rollout lifecycle transitions natively.
func HandleDeploymentWebhook(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload WebhookPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload format: " + err.Error()})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		query := `UPDATE deployments SET status = $1, job_id = $2, logs = $3, updated_at = NOW() WHERE id = $4`
		result, err := db.ExecContext(ctx, query, payload.Status, payload.JobID, payload.Logs, payload.DeploymentID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed executing deployment update map: " + err.Error()})
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil || rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "deployment mapping not found in active database"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "deployment updated successfully",
			"status":  payload.Status,
			"id":      payload.DeploymentID,
			"job_id":  payload.JobID,
		})
	}
}
