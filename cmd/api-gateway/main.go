// cmd/api-gateway/main.go
// Entry point for the cmp-core API Gateway microservice.

// @title           CMP-Core API
// @version         1.0
// @description     Cloud Management Platform — multi-tenant API Gateway.
// @host            localhost:8080
// @BasePath        /
// @schemes         http

// @securityDefinitions.apikey BearerAuth
// @in                         header
// @name                       Authorization
// @description                Enter your JWT as: Bearer <token>

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ifeoluwashola/cmp-core/internal/api"
	"github.com/ifeoluwashola/cmp-core/internal/auth"
	"github.com/ifeoluwashola/cmp-core/internal/cicd"
	"github.com/ifeoluwashola/cmp-core/internal/database"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// ── Database ──────────────────────────────────────────────────────────────
	connStr := buildConnString()
	pool, err := database.NewPool(ctx, connStr)
	if err != nil {
		log.Fatalf("api-gateway: database: %v", err)
	}
	defer pool.Close()
	
	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
	    log.Fatalf("api-gateway: sql database driver mapping failed: %v", err)
	}
	defer sqlDB.Close()
	log.Println("api-gateway: database pool ready")

	// ── JWT Manager ───────────────────────────────────────────────────────────
	secret := getEnv("JWT_SECRET", "")
	if secret == "" {
		log.Fatal("api-gateway: JWT_SECRET must be set")
	}
	expiryHours, _ := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", "24"))
	jwtManager := auth.NewManager(secret, expiryHours)

	// ── CI/CD Provider ────────────────────────────────────────────────────────
	// Swap cicd.NewMockProvider() for a real implementation once a CI/CD
	// system (GitHub Actions, GitLab CI, …) is configured.
	cicdProvider := cicd.NewMockProvider()

	// ── Webhook Secret ────────────────────────────────────────────────────────
	webhookSecret := getEnv("CMP_WEBHOOK_SECRET", "")
	if webhookSecret == "" {
		log.Fatal("api-gateway: CMP_WEBHOOK_SECRET must be set")
	}

	// ── Router & Server ───────────────────────────────────────────────────────
	router := api.SetupRouter(pool, sqlDB, jwtManager, cicdProvider, webhookSecret)

	addr := ":" + getEnv("PORT", "8080")
	log.Printf("api-gateway: listening on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("api-gateway: server error: %v", err)
	}
}


func buildConnString() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	user := getEnv("DB_USER", "")
	name := getEnv("DB_NAME", "")
	if user == "" || name == "" {
		log.Fatal("api-gateway: DB_USER and DB_NAME must be set")
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user,
		getEnv("DB_PASSWORD", ""),
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		name,
		getEnv("DB_SSLMODE", "disable"),
	)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
