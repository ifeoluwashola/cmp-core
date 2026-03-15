// internal/api/router.go
// API router wiring for the cmp-core API Gateway.

package api

import (
	"github.com/gin-gonic/gin"
	"github.com/ifeoluwashola/cmp-core/internal/auth"
	"github.com/ifeoluwashola/cmp-core/internal/handlers"
	"github.com/ifeoluwashola/cmp-core/internal/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/ifeoluwashola/cmp-core/docs"
)

// SetupRouter creates the Gin engine with all routes and middleware registered.
func SetupRouter(pool *pgxpool.Pool, jwtManager *auth.Manager) *gin.Engine {
	r := gin.New()

	// ── Global middleware ─────────────────────────────────────────────────────
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// ── Health check & Swagger (no auth) ──────────────────────────────────────
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// ── Public identity routes (no JWT required) ───────────────────────────────
	identityHandler := handlers.NewIdentityHandler(pool, jwtManager)
	r.POST("/register", identityHandler.Register)
	r.POST("/login",    identityHandler.Login)

	// ── Protected API v1 group (JWT required) ─────────────────────────────────
	v1 := r.Group("/api/v1")
	v1.Use(middleware.TenantMiddleware(jwtManager))

	// Cloud Environments
	envHandler := handlers.NewCloudEnvHandler(pool)
	envRoutes := v1.Group("/environments")
	{
		envRoutes.POST("", envHandler.Create)
		envRoutes.GET("", envHandler.List)
	}

	return r
}
