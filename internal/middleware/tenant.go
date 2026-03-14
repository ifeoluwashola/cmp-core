// internal/middleware/tenant.go
// Gin middleware for multi-tenant request scoping.
//
// Every authenticated API request must carry the organization it is acting on
// behalf of. This middleware:
//  1. Reads the X-Organization-ID header.
//  2. Validates it as a well-formed UUID.
//  3. Injects the parsed uuid.UUID into both Gin's key-value store and the
//     request's context.Context so it can be forwarded into WithOrgTx.
//
// Wire this middleware after your authentication middleware so that JWTs have
// already been verified before we trust the organization claim.

package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// orgIDKey is an unexported type used as the context key to avoid collisions
// with keys from other packages.
type orgIDKey struct{}

const headerOrgID = "X-Organization-ID"

// TenantMiddleware returns a Gin handler that enforces the presence of a valid
// organization UUID in the X-Organization-ID request header.
//
// On success the parsed uuid.UUID is available via:
//   - c.MustGet(middleware.OrgIDKey).(uuid.UUID)   — Gin-style
//   - middleware.OrgIDFromContext(c.Request.Context()) — stdlib context style
//
// Example registration:
//
//	r := gin.New()
//	r.Use(middleware.TenantMiddleware())
func TenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(headerOrgID)
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "missing required header: " + headerOrgID,
			})
			return
		}

		orgID, err := uuid.Parse(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "invalid " + headerOrgID + " header: must be a valid UUID",
			})
			return
		}

		// Store in Gin's context for handlers that use c.MustGet.
		c.Set(OrgIDKey, orgID)

		// Also inject into the stdlib context so it survives hand-off to
		// non-Gin code (e.g., service layer, database.WithOrgTx).
		ctx := context.WithValue(c.Request.Context(), orgIDKey{}, orgID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// OrgIDKey is the Gin context key under which the parsed organization UUID is stored.
// Use c.MustGet(middleware.OrgIDKey).(uuid.UUID) in handlers.
const OrgIDKey = "organizationID"

// OrgIDFromContext retrieves the organization UUID previously injected by
// TenantMiddleware from a stdlib context.Context.
// Returns (uuid.Nil, false) if the value is absent.
func OrgIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v := ctx.Value(orgIDKey{})
	if v == nil {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}
