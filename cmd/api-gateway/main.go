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
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ifeoluwashola/cmp-core/internal/api"
	"github.com/ifeoluwashola/cmp-core/internal/auth"
	"github.com/ifeoluwashola/cmp-core/internal/database"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// ── Database ──────────────────────────────────────────────────────────────
	pool, err := database.NewPool(ctx, buildConnString())
	if err != nil {
		log.Fatalf("api-gateway: database: %v", err)
	}
	defer pool.Close()
	log.Println("api-gateway: database pool ready")

	// ── JWT Manager ───────────────────────────────────────────────────────────
	secret := getEnv("JWT_SECRET", "")
	if secret == "" {
		log.Fatal("api-gateway: JWT_SECRET must be set")
	}
	expiryHours, _ := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", "24"))
	jwtManager := auth.NewManager(secret, expiryHours)

	// ── Router & Server ───────────────────────────────────────────────────────
	router := api.SetupRouter(pool, jwtManager)

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
