// cmd/auditing-service/main.go
// Auditing Service microservice entrypoint.
//
// This service runs as a standalone process alongside the API Gateway.
// It connects to PostgreSQL using the same DB_* env vars but logs in as
// the owner role (which can SET LOCAL ROLE cmp_service via WithServiceTx).

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	awscloud "github.com/ifeoluwashola/cmp-core/internal/cloud/aws"
	"github.com/ifeoluwashola/cmp-core/internal/cloud"
	"github.com/ifeoluwashola/cmp-core/internal/database"
	"github.com/ifeoluwashola/cmp-core/internal/models"
	"github.com/ifeoluwashola/cmp-core/internal/worker"
)

const auditInterval = 1 * time.Minute

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Database connection ───────────────────────────────────────────────────
	connString := buildConnString()

	// Use a shorter startup timeout so misconfiguration fails fast.
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := database.NewPool(initCtx, connString)
	cancel()
	if err != nil {
		log.Fatalf("auditing-service: connect to database: %v", err)
	}
	defer pool.Close()
	log.Println("auditing-service: database pool ready")

	// ── Cloud provider registry ───────────────────────────────────────────────
	// RealFetcher uses the AWS SDK v2 with cross-account AssumeRole.
	// If a CloudEnvironment has a RoleARN set, the auditor will assume that
	// role before making API calls — no long-lived keys needed on the platform.
	registry := cloud.Registry{
		models.CloudProviderAWS: awscloud.NewRealFetcher(),
		// models.CloudProviderGCP:   gcp.NewFetcher(...),
		// models.CloudProviderAzure: azure.NewFetcher(...),
	}

	// ── Auditor ───────────────────────────────────────────────────────────────
	auditor := worker.NewAuditor(pool, registry)

	infraTicker := time.NewTicker(auditInterval)
	defer infraTicker.Stop()

	costTicker := time.NewTicker(24 * time.Hour)
	defer costTicker.Stop()

	log.Printf("auditing-service: starting resource audit loop (interval: %s)", auditInterval)
	go auditor.StartResourceAuditor(ctx, infraTicker)

	log.Printf("auditing-service: triggering initial cost sync now")
	go func() {
		if err := auditor.RunCostCycle(ctx); err != nil {
			log.Printf("auditing-service: initial cost sync failed: %v", err)
		}
		// Then hand over to the 24-hour background ticker
		auditor.StartCostAuditor(ctx, costTicker)
	}()

	// Block until OS signal or context cancellation.
	<-ctx.Done()
	log.Println("auditing-service: shutdown signal received, exiting cleanly")
}

func buildConnString() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "")
	pass := getEnv("DB_PASSWORD", "")
	name := getEnv("DB_NAME", "")
	ssl  := getEnv("DB_SSLMODE", "disable")

	if user == "" || name == "" {
		log.Fatal("auditing-service: DB_USER and DB_NAME must be set")
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, pass, host, port, name, ssl)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
