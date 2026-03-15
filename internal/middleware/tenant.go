// internal/middleware/tenant.go
// Gin middleware for JWT-based multi-tenant request scoping.
//
// Every authenticated API request must carry a valid JWT in the
// Authorization header. The token was issued by POST /login and contains
// the organization_id, user_id, and role as claims — removing the need
// for a separate X-Organization-ID header or any DB lookup per request.

package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ifeoluwashola/cmp-core/internal/auth"
)

// ─── context keys (unexported to prevent collisions) ─────────────────────────

type orgIDKey  struct{}
type userIDKey struct{}
type roleKey   struct{}

// ─── Gin context keys (string constants for c.MustGet / c.Set) ───────────────

const (
	OrgIDKey  = "organizationID"
	UserIDKey = "userID"
	RoleKey   = "role"
)

// TenantMiddleware returns a Gin handler that:
//  1. Reads the Authorization: Bearer <token> header.
//  2. Validates the JWT using the provided Manager.
//  3. Injects organization_id, user_id, and role into both gin.Context and
//     the stdlib context.Context for downstream use (e.g., WithOrgTx).
func TenantMiddleware(jwtManager *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing or malformed Authorization header (expected: Bearer <token>)",
			})
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := jwtManager.Validate(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token: " + err.Error(),
			})
			return
		}

		// ── Inject into Gin context ───────────────────────────────────────────
		c.Set(OrgIDKey,  claims.OrganizationID)
		c.Set(UserIDKey, claims.UserID)
		c.Set(RoleKey,   claims.Role)

		// ── Inject into stdlib context (for WithOrgTx) ────────────────────────
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, orgIDKey{},  claims.OrganizationID)
		ctx = context.WithValue(ctx, userIDKey{}, claims.UserID)
		ctx = context.WithValue(ctx, roleKey{},   claims.Role)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// ─── Context extractors ───────────────────────────────────────────────────────

// OrgIDFromContext retrieves the organization UUID from the request context.
func OrgIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(orgIDKey{}).(uuid.UUID)
	return v, ok
}

// UserIDFromContext retrieves the user UUID from the request context.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return v, ok
}

// RoleFromContext retrieves the user role string from the request context.
func RoleFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(roleKey{}).(string)
	return v, ok
}
