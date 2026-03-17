// internal/api/router.go
// API router wiring for the cmp-core API Gateway.

package api

import (
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/ifeoluwashola/cmp-core/internal/auth"
	"github.com/ifeoluwashola/cmp-core/internal/cicd"
	corehandlers "github.com/ifeoluwashola/cmp-core/internal/handlers"
	"github.com/ifeoluwashola/cmp-core/internal/api/handlers"
	"github.com/ifeoluwashola/cmp-core/internal/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"database/sql"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/ifeoluwashola/cmp-core/docs"
)

// SetupRouter creates the Gin engine with all routes and middleware registered.
func SetupRouter(pool *pgxpool.Pool, sqlDB *sql.DB, jwtManager *auth.Manager, cicdProvider cicd.PipelineProvider, webhookSecret string) *gin.Engine {
	r := gin.New()

	// ── Global middleware ─────────────────────────────────────────────────────
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	corsConfig := cors.DefaultConfig()
	allowedOrigin := os.Getenv("ALLOWED_ORIGIN")
	if allowedOrigin == "" {
		allowedOrigin = "http://localhost:3000" // Default fallback
	}
	corsConfig.AllowOrigins = []string{allowedOrigin} // Dynamic Frontend origin
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization", "Accept", "ngrok-skip-browser-warning"}
	corsConfig.AllowCredentials = true
	r.Use(cors.New(corsConfig))

	// ── Health check & Swagger (no auth) ──────────────────────────────────────
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// ── Public identity routes (no JWT required) ───────────────────────────────
	// ── Public identity routes (no JWT required) ───────────────────────────────
	identityHandler := corehandlers.NewIdentityHandler(pool, jwtManager)
	r.POST("/register", identityHandler.Register)
	r.POST("/login",    identityHandler.Login)

	// ── Webhook routes (no JWT — validated by shared secret) ──────────────────
	deploymentHandler := corehandlers.NewDeploymentHandler(pool, cicdProvider)
	webhooks := r.Group("/api/v1/webhooks")
	webhooks.Use(middleware.WebhookSecretMiddleware(webhookSecret))
	{
		webhooks.POST("/cicd", deploymentHandler.WebhookCallback)
		webhooks.POST("/deployments", handlers.HandleDeploymentWebhook(sqlDB))
	}

	// ── Protected API v1 group (JWT required) ─────────────────────────────────
	v1 := r.Group("/api/v1")
	v1.Use(middleware.TenantMiddleware(jwtManager))

	// Cloud Environments
	envHandler := corehandlers.NewCloudEnvHandler(pool)
	envRoutes := v1.Group("/environments")
	{
		envRoutes.POST("", envHandler.Create)
		envRoutes.GET("",  envHandler.List)
	}

	// Infrastructure Resources
	infraHandler := corehandlers.NewInfrastructureHandler(pool)
	v1.GET("/infrastructure", infraHandler.List)

	// FinOps / Cost Analytics
	finopsHandler := corehandlers.NewFinOpsHandler(pool)
	v1.GET("/costs/summary", finopsHandler.GetCostSummary)

	// Provisioning / Deployments
	v1.POST("/deployments",     deploymentHandler.TriggerDeployment)
	v1.GET("/deployments",      deploymentHandler.ListDeployments)
	v1.GET("/deployments/:id",  deploymentHandler.GetDeployment)


	return r
}

