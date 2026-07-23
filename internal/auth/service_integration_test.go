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

	return pool
}

// uniqueEmail returns an email unique to this test run. DB-backed tests isolate
// on it instead of a global TRUNCATE: two test binaries (this package and
// cmd/server) run in parallel against the same tables, so a shared TRUNCATE
// would clobber each other's rows. The UUID also survives repeated -count runs
// against a persistent dev database.
func uniqueEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%s@example.test", uuid.NewString())
}

// cleanupUser deletes the user behind email when the test finishes, cascading
// to their credential and progress rows, so a persistent dev DB stays tidy.
// Harmless when no such user exists (e.g. a rolled-back registration).
func cleanupUser(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Helper()
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(),
			`DELETE FROM users WHERE id IN
			   (SELECT user_id FROM auth_credentials WHERE identifier = $1)`, email)
		if err != nil {
			t.Errorf("cleanup: deleting user for %s: %v", email, err)
		}
	})
}

func newTestService(t *testing.T) (svc *auth.Service, authn *auth.Authenticator, pool *pgxpool.Pool) {
	t.Helper()
	pool = newTestPool(t)
	authn = auth.NewAuthenticator([]byte("test-secret"), time.Hour)
	svc = auth.NewService(pool, realProgress, authn, newTestHasher(t))
	return svc, authn, pool
}

// newTestHasher builds a Hasher with a small concurrency bound for tests.
func newTestHasher(t *testing.T) *auth.Hasher {
	t.Helper()
	h, err := auth.NewHasher(2)
	if err != nil {
		t.Fatalf("failed to build hasher: %v", err)
	}
	return h
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
	email := uniqueEmail(t)
	cleanupUser(t, pool, email)

	tok, err := svc.Register(ctx, email, "correct horse battery staple")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tok.Value == "" {
		t.Errorf("token value unexpectedly empty.")
	}

	// Scoped to this email: the credential exists (else no row -> ErrNoRows),
	// references a real user, and that user has a progress row - i.e. all three
	// rows were written together in the registration tx.
	var hasProgress bool
	err = pool.QueryRow(ctx, `
		SELECT p.user_id IS NOT NULL
		FROM auth_credentials c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN user_progress p ON p.user_id = u.id
		WHERE c.identifier = $1 AND c.cred_kind = 'password'`, email).Scan(&hasProgress)
	if err != nil {
		t.Fatalf("querying registered rows for %s: %v", email, err)
	}
	if !hasProgress {
		t.Error("user_progress row missing for the registered user")
	}
}

func TestRegister_Integration_DuplicateEmail(t *testing.T) {
	svc, _, pool := newTestService(t)
	ctx := context.Background()
	email := uniqueEmail(t)
	cleanupUser(t, pool, email)

	tok, err := svc.Register(ctx, email, "correct horse battery staple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Value == "" {
		t.Fatalf("token value unexpectedly empty.")
	}

	// same email, different password
	_, err = svc.Register(ctx, email, "another password entirely")
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("expected error 'ErrEmailTaken', got: %v", err)
	}
}

func TestRegister_Integration_RollsBackOnProgressFailure(t *testing.T) {
	_, _, pool := newTestService(t)
	ctx := context.Background()
	email := uniqueEmail(t)
	cleanupUser(t, pool, email) // no-op if the rollback worked; a safety net if it didn't
	svc := auth.NewService(
		pool,
		failingProgressFactory,
		auth.NewAuthenticator([]byte("test-secret"), time.Hour),
		newTestHasher(t),
	)

	_, err := svc.Register(ctx, email, "correct horse battery staple")
	if err == nil {
		t.Errorf("expected error, got none.")
	}

	// The credential insert and the failing progress insert share one tx, so a
	// rolled-back registration must leave no credential for this email - and, by
	// the same tx, no user or progress row either.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM auth_credentials WHERE identifier = $1`, email).Scan(&n); err != nil {
		t.Fatalf("counting credentials for %s: %v", email, err)
	}
	if n != 0 {
		t.Errorf("expected 0 credentials for %s after rollback, got %d", email, n)
	}
}

func TestLogin_Integration_HappyPath(t *testing.T) {
	svc, authn, pool := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail(t)
	cleanupUser(t, pool, email)
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
	svc, _, pool := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail(t)
	cleanupUser(t, pool, email)
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
	svc, _, pool := newTestService(t)
	ctx := context.Background()

	email := uniqueEmail(t)
	cleanupUser(t, pool, email)
	password := "correct horse battery staple"
	wrongEmail := uniqueEmail(t) // never registered

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
