// internal/middleware/webhook.go
// Webhook secret authentication middleware.
//
// The CI/CD callback endpoint (POST /api/v1/webhooks/cicd) is not protected
// by a user JWT — instead it validates a pre-shared secret passed via the
// X-Webhook-Secret request header. The secret is configured at startup
// via the WEBHOOK_SECRET environment variable.

package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// WebhookSecretMiddleware returns a Gin handler that rejects requests whose
// X-Webhook-Secret header doesn't match the configured secret.
// Abort with 401 so callers know the request was not authorised (not 403,
// which would imply the caller is authenticated but lacks permission).
func WebhookSecretMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-Webhook-Secret")
		if provided == "" || provided != secret {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or missing X-Webhook-Secret header",
			})
			return
		}
		c.Next()
	}
}
