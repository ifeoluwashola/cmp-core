// internal/database/db.go
// PostgreSQL connection pool using pgx/v5's pgxpool.
// Call NewPool once at application startup and share the returned pool across
// all services; pgxpool is safe for concurrent use.

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMaxConns          = 25
	defaultMinConns          = 2
	defaultMaxConnIdleTime   = 30 * time.Second
	defaultMaxConnLifetime   = 5 * time.Minute
	defaultHealthCheckPeriod = 1 * time.Minute
	defaultConnectTimeout    = 5 * time.Second
)

// NewPool creates a pgxpool.Pool from the provided connection string.
// connString follows the libpq URI format:
//
//	postgres://user:pass@host:port/dbname?sslmode=disable
//
// The pool is configured for a typical multi-tenant API workload; callers
// can clone and modify the returned config before calling pgxpool.NewWithConfig
// if they need custom settings.
func NewPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}

	cfg.MaxConns = defaultMaxConns
	cfg.MinConns = defaultMinConns
	cfg.MaxConnIdleTime = defaultMaxConnIdleTime
	cfg.MaxConnLifetime = defaultMaxConnLifetime
	cfg.HealthCheckPeriod = defaultHealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = defaultConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	// Eagerly verify connectivity so startup fails fast on misconfiguration.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return pool, nil
}
