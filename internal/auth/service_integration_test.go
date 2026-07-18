package auth_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/platform/database"
	"github.com/corygyarmathy/typist/internal/progress"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool opens a pool against DATABASE_URL, migrates, and truncates the
// auth/progress tables so each test starts from a known-empty database.
// Skips entirely when DATABASE_URL is unset (so local `make test` stays green).
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("integration test: set DATABASE_URL (make docker-up)")
	}
	ctx := context.Background()

	pool, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to open database pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	err = database.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("failed to apply database migrations: %v", err)
	}

	// Reset database rows to clean slate for testing
	_, err = pool.Exec(ctx, "TRUNCATE users, auth_credentials, user_progress RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("failed to truncate database rows: %v", err)
	}

	return pool
}

// realProgress mirrors cmd/server's newProgressInitialiser - the production factory.
func realProgress(tx pgx.Tx) auth.ProgressInitialiser {
	return progress.NewInitialiser(tx)
}

type failingProgress struct{}

func (failingProgress) CreateInitial(ctx context.Context, userID uuid.UUID) error {
	return errors.New("boom: simulated progress failure")
}

func failingProgressFactory(tx pgx.Tx) auth.ProgressInitialiser {
	return failingProgress{}
}

func TestRegister_Integration_HappyPath(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	authn := auth.NewAuthenticator([]byte("test-secret"), time.Hour)
	svc := auth.NewService(pool, realProgress, authn)

	tok, err := svc.Register(ctx, "alice@example.com", "correct horse battery staple")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tok.Value == "" {
		t.Errorf("token value unexpectedly empty.")
	}

	tables := []string{"users", "auth_credentials", "user_progress"}
	var n int
	for _, table := range tables {
		query := fmt.Sprintf("SELECT count(*) FROM %s", table)
		err = pool.QueryRow(ctx, query).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count %s table rows: %v", table, err)
		}
		if n != 1 {
			t.Errorf("expected 1 row in %s table, got %d", table, n)
		}
	}
}

func TestRegister_Integration_DuplicateEmail(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	svc := auth.NewService(pool, realProgress, auth.NewAuthenticator([]byte("test-secret"), time.Hour))

	tok, err := svc.Register(ctx, "alice@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Value == "" {
		t.Fatalf("token value unexpectedly empty.")
	}

	// same email, different password
	_, err = svc.Register(ctx, "alice@example.com", "another password entirely")
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("expected error 'ErrEmailTaken', got: %v", err)
	}
}

func TestRegister_Integration_RollsBackOnProgressFailure(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	svc := auth.NewService(pool, failingProgressFactory, auth.NewAuthenticator([]byte("test-secret"), time.Hour))

	_, err := svc.Register(ctx, "alice@example.com", "correct horse battery staple")
	if err == nil {
		t.Errorf("expected error, got none.")
	}

	tables := []string{"users", "auth_credentials", "user_progress"}
	var n int
	for _, table := range tables {
		query := fmt.Sprintf("SELECT count(*) FROM %s", table)
		err = pool.QueryRow(ctx, query).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count %s table rows: %v", table, err)
		}
		if n != 0 {
			t.Errorf("expected 0 row in %s table, got %d", table, n)
		}
	}
}
