// Package database provides the pgx connection pool and migration runner.
//
// The package exposes a minimal surface: Open returns a *pgxpool.Pool wired
// to the configured database, and Migrate runs all pending goose migrations
// from the embedded migrations directory.
//
// Repositories elsewhere in the codebase accept *pgxpool.Pool (or the
// narrower pgx.Tx for transactional work) rather than this package directly,
// so they remain testable with testcontainers.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/corygyarmathy/typist/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Open constructs a database pool from the given connection string and
// verifies connectivity before returning. The caller owns the pool and
// must Close it on shutdown.
func Open(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return &pgxpool.Pool{}, fmt.Errorf("parsing db connString config: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return &pgxpool.Pool{}, fmt.Errorf("creating pgxpool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return &pgxpool.Pool{}, fmt.Errorf("pinging database: %w", err)
	}

	return pool, nil
}

// Migrate applies all pending migrations from the embedded migrations dir.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// goose speaks database/sql; adapt the pgx pool rather than opening a
	// second connection path with its own credentials.
	db := stdlib.OpenDBFromPool(pool)
	defer func() {
		if cerr := db.Close(); cerr != nil {
			slog.Error("closing database:", "cerr", cerr)
		}
	}()

	// prevents multiple instances from running simultaneous migrations
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("creating migration locker: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations.SQLMigrationsFS, // uses embedded migrations dir
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return fmt.Errorf("creating migration provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	return nil
}
