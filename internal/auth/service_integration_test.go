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

func newTestService(t *testing.T) (svc *auth.Service, authn *auth.Authenticator, pool *pgxpool.Pool) {
	t.Helper()
	pool = newTestPool(t)
	authn = auth.NewAuthenticator([]byte("test-secret"), time.Hour)
	svc = auth.NewService(pool, realProgress, authn)
	return svc, authn, pool
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
	svc, _, pool := newTestService(t)
	ctx := context.Background()

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
	svc, _, _ := newTestService(t)
	ctx := context.Background()

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
	_, _, pool := newTestService(t)
	ctx := context.Background()
	svc := auth.NewService(
		pool,
		failingProgressFactory,
		auth.NewAuthenticator([]byte("test-secret"), time.Hour),
	)

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

func TestLogin_Integration_HappyPath(t *testing.T) {
	svc, authn, _ := newTestService(t)
	ctx := context.Background()

	const email = "alice@example.com"
	const password = "correct horse battery staple"

	if _, err := svc.Register(ctx, email, password); err != nil {
		t.Fatalf("register (arrange step) failed: %v", err)
	}

	token, err := svc.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("unexpectedly received error on Login: %v", err)
	}
	if token == (auth.Token{}) {
		t.Fatalf("Login attempt unexpectedly returned empty Token")
	}

	userID, err := authn.Validate(token.Value)
	if err != nil {
		t.Fatalf("unexpectedly received error on Token Validate: %v", err)
	}
	if userID == uuid.Nil {
		t.Fatalf("Validate returned nil userID: %v", userID)
	}
}

func TestLogin_Integration_WrongPassword(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	const email = "alice@example.com"
	const password = "correct horse battery staple"
	const wrongPassword = "this is the wrong password"

	if _, err := svc.Register(ctx, email, password); err != nil {
		t.Fatalf("register (arrange step) failed: %v", err)
	}

	token, err := svc.Login(ctx, email, wrongPassword)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("received unexpected err. Expected '%v', got '%v'", auth.ErrInvalidCredentials, err)
	}
	if token != (auth.Token{}) {
		t.Fatalf("unexpectedly received non-empty Token")
	}
}

func TestLogin_Integration_WrongEmail(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	const email = "alice@example.com"
	const password = "correct horse battery staple"
	const wrongEmail = "bob@example.com"

	if _, err := svc.Register(ctx, email, password); err != nil {
		t.Fatalf("register (arrange step) failed: %v", err)
	}

	token, err := svc.Login(ctx, wrongEmail, password)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("received unexpected err. Expected '%v', got '%v'", auth.ErrInvalidCredentials, err)
	}
	if token != (auth.Token{}) {
		t.Fatalf("unexpectedly received non-empty Token")
	}
}
