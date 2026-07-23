package main

import (
	"context"
	"encoding/json"
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

func TestE2E_RegisterLoginProgress(t *testing.T) {
	pool := newTestPool(t)

	// Build the main wiring main.go builds
	authn := auth.NewAuthenticator([]byte("test-secret"), time.Hour)
	authSvc := auth.NewService(pool, newProgressInitialiser, authn)
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

	// 1. Register -> 200; decode TokenResponse; token non-empty
	registerBody := `{"email":"alice@example.com","password":"correct horse battery staple"}`
	rec := do(http.MethodPost, "/auth/register", registerBody, "") // no token yet
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
	loginBody := `{"email":"alice@example.com","password":"correct horse battery staple"}`
	rec = do(http.MethodPost, "/auth/login", loginBody, "") // no token yet
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
