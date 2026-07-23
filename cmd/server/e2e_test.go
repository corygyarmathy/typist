package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/openapi"
	"github.com/corygyarmathy/typist/internal/platform/database"
	"github.com/corygyarmathy/typist/internal/progress"
	"github.com/google/uuid"
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
// on it instead of a global TRUNCATE: this package and internal/auth run as
// separate test binaries in parallel against the same tables, so a shared
// TRUNCATE would clobber each other's rows. The UUID also survives repeated
// -count runs against a persistent dev database.
func uniqueEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%s@example.test", uuid.NewString())
}

// cleanupUser deletes the user behind email when the test finishes, cascading
// to their credential and progress rows, so a persistent dev DB stays tidy.
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

func TestE2E_RegisterLoginProgress(t *testing.T) {
	pool := newTestPool(t)

	// Build the main wiring main.go builds
	authn := auth.NewAuthenticator([]byte("test-secret"), time.Hour)
	hasher, err := auth.NewHasher(2)
	if err != nil {
		t.Fatalf("failed to build hasher: %v", err)
	}
	authSvc := auth.NewService(pool, newProgressInitialiser, authn, hasher)
	progressSvc := progress.NewService(pool)
	api := &API{
		ready:    pool.Ping,
		auth:     auth.NewHandler(authSvc),
		progress: progress.NewHandler(progressSvc),
	}
	router := Router(api, authn)

	// helper: marshal a body, build the request, serve, return the recorder
	do := func(httpMethod, target, body, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(httpMethod, target, strings.NewReader(body))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	email := uniqueEmail(t)
	cleanupUser(t, pool, email)
	body := fmt.Sprintf(`{"email":%q,"password":"correct horse battery staple"}`, email)

	// 1. Register -> 200; decode TokenResponse; token non-empty
	rec := do(http.MethodPost, "/auth/register", body, "") // no token yet
	if rec.Code != http.StatusOK {
		t.Fatalf("register: status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var regToken openapi.TokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&regToken); err != nil {
		t.Fatalf("register: decode response: %v", err)
	}
	if regToken.Token == "" {
		t.Fatal("register: got empty token")
	}

	// 2. Login   -> 200; decode; token non-empty
	rec = do(http.MethodPost, "/auth/login", body, "") // same credentials, no token yet
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %v (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var loginToken openapi.TokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&loginToken); err != nil {
		t.Fatalf("login: decode response: %v", err)
	}
	if loginToken.Token == "" {
		t.Fatal("login: got empty token")
	}

	// 3. GET /progress WITH the token -> 200, body is the seeded competency JSON
	rec = do(http.MethodGet, "/progress", "", loginToken.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("progress: status = %d, want %v (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var progressRes map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&progressRes); err != nil {
		t.Fatalf("progress: decode response: %v", err)
	}
	if len(progressRes) == 0 {
		t.Fatal("progress: got empty progress response")
	}

	// 4. GET /progress with a garbage token -> 401 problem+json
	rec = do(http.MethodGet, "/progress", "", "not-a-real-jwt")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("progress: status = %d, want %v (body: %s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("progress bad token: 'Content-Type' = %s, want 'application/problem+json'", rec.Header().Get("Content-Type"))
	}

	var probRes map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&probRes); err != nil {
		t.Fatalf("progress: decode response: %v", err)
	}
	if len(probRes) == 0 {
		t.Fatal("progress: got empty progress response")
	}

	// 5. GET /progress with NO token → 401
	rec = do(http.MethodGet, "/progress", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("progress: status = %d, want %v (body: %s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("progress empty token: 'Content-Type' = %s, want 'application/problem+json'", rec.Header().Get("Content-Type"))
	}
}
