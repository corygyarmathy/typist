package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/corpus"
	"github.com/corygyarmathy/typist/internal/openapi"
	"github.com/corygyarmathy/typist/internal/platform/database"
	"github.com/corygyarmathy/typist/internal/progress"
	"github.com/corygyarmathy/typist/internal/session"
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

// newTestRouter builds exactly the wiring main.go builds. An e2e test that
// hand-assembled a subset would pass while the real composition root was
// broken, which is the one failure this level of test exists to catch.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) (http.Handler, *auth.Authenticator) {
	t.Helper()
	// Build the main wiring main.go builds
	authn := auth.NewAuthenticator([]byte("test-secret"), time.Hour)
	hasher, err := auth.NewHasher(2)
	if err != nil {
		t.Fatalf("failed to build hasher: %v", err)
	}
	corpusProvider, err := corpus.New()
	if err != nil {
		t.Fatalf("failed to load corpus: %v", err)
	}

	authSvc := auth.NewService(pool, newProgressInitialiser(corpusProvider), authn, hasher)
	progressSvc := progress.NewService(pool, corpusProvider)
	sessionSvc := session.NewService(pool, newCompetencyStore, corpusProvider)

	api := &API{
		ready:    pool.Ping,
		auth:     auth.NewHandler(authSvc),
		progress: progress.NewHandler(progressSvc),
		session:  session.NewHandler(sessionSvc),
	}
	return Router(api, authn), authn
}

// helper: marshal a body, build the request, serve, return the recorder
func newResponseRecorder(router http.Handler) func(string, string, string, string) *httptest.ResponseRecorder {
	return func(httpMethod, target, body, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(httpMethod, target, strings.NewReader(body))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
}

func setupE2ETest(t *testing.T) (
	*pgxpool.Pool,
	http.Handler,
	*auth.Authenticator,
	func(string, string, string, string) *httptest.ResponseRecorder,
) {
	// 0. Setup
	pool := newTestPool(t)
	router, authn := newTestRouter(t, pool)
	do := newResponseRecorder(router)

	return pool, router, authn, do
}

func userRegisterLogin(
	t *testing.T,
	pool *pgxpool.Pool,
	do func(string, string, string, string) *httptest.ResponseRecorder,
) (loginToken openapi.TokenResponse) {
	email := uniqueEmail(t)
	cleanupUser(t, pool, email)
	body := fmt.Sprintf(`{"email":%q,"password":"correct horse battery staple"}`, email)

	// 1. Register -> 200; decode TokenResponse; token non-empty
	rec := do(http.MethodPost, "/api/v1/auth/register", body, "") // no token yet
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
	rec = do(http.MethodPost, "/api/v1/auth/login", body, "") // same credentials, no token yet
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %v (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	if err := json.NewDecoder(rec.Body).Decode(&loginToken); err != nil {
		t.Fatalf("login: decode response: %v", err)
	}
	if loginToken.Token == "" {
		t.Fatal("login: got empty token")
	}

	return loginToken
}

func TestE2E_RegisterLoginProgressLesson(t *testing.T) {
	pool, _, _, do := setupE2ETest(t)
	loginToken := userRegisterLogin(t, pool, do)

	// 3. GET /api/v1/progress WITH the token -> 200, body is the seeded competency JSON
	rec := do(http.MethodGet, "/api/v1/progress", "", loginToken.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("progress: status = %d, want %v (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		Keys map[string]struct {
			Score   float64 `json:"score"`
			Samples int     `json:"samples"`
		} `json:"keys"`
		Ngrams    map[string]any `json:"ngrams"`
		NgramTier int            `json:"ngram_tier"`
		TargetWPM int            `json:"target_wpm"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("progress: decode response: %v", err)
	}

	if got.NgramTier != 20 {
		t.Errorf("ngram_tier = %d, want 20", got.NgramTier)
	}
	if got.TargetWPM != 40 {
		t.Errorf("target_wpm = %d, want 40", got.TargetWPM)
	}
	if len(got.Keys) != 4 {
		t.Errorf("len(keys) = %d, want 4", len(got.Keys))
	}
	for _, k := range []string{"e", "t", "a", "o"} {
		if _, ok := got.Keys[k]; !ok {
			t.Errorf("keys is missing %q; got %v", k, slices.Sorted(maps.Keys(got.Keys)))
		}
	}
	if len(got.Ngrams) != 0 {
		t.Errorf("len(ngrams) = %d, want 0", len(got.Ngrams))
	}

	// 4. GET /api/v1/progress with a garbage token -> 401 problem+json
	rec = do(http.MethodGet, "/api/v1/progress", "", "not-a-real-jwt")
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

	// 5. GET /api/v1/progress with NO token → 401
	rec = do(http.MethodGet, "/api/v1/progress", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("progress: status = %d, want %v (body: %s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("progress empty token: 'Content-Type' = %s, want 'application/problem+json'", rec.Header().Get("Content-Type"))
	}

	// 6. GET /api/v1/lessons/next WITH the token -> 200; every character of
	// every word is a key this user has already unlocked.
	//
	// This is phase 3's engine invariant (a lesson never uses a locked key)
	// re-asserted at the HTTP boundary, which is where it first matters to
	// someone outside the engine package. It deliberately does not pin the
	// words themselves: the handler seeds a fresh generator per request
	// (Decision 7), so the lesson is different every run and only the
	// invariant is stable. Determinism is pinned one layer down, in
	// internal/progress/service_test.go, where the seed can be injected.
	rec = do(http.MethodGet, "/api/v1/lessons/next", "", loginToken.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("next lesson: status = %d, want %v (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("next lesson: 'Content-Type' = %s, want 'application/json'", rec.Header().Get("Content-Type"))
	}

	var lesson openapi.Lesson
	if err := json.NewDecoder(rec.Body).Decode(&lesson); err != nil {
		t.Fatalf("next lesson: decode response: %v", err)
	}

	// Non-nil, not merely non-empty: api/openapi.yaml declares both arrays
	// required, and a nil slice marshals as null rather than [].
	if lesson.Words == nil {
		t.Fatal("next lesson: words is null, want an array")
	}
	if lesson.Targets == nil {
		t.Error("next lesson: targets is null, want an array")
	}

	unlocked := slices.Sorted(maps.Keys(got.Keys))
	for _, word := range lesson.Words {
		for _, r := range word {
			if _, ok := got.Keys[string(r)]; !ok {
				t.Errorf("next lesson: word %q uses key %q, which is not unlocked; unlocked keys are %v",
					word, string(r), unlocked)
			}
		}
	}

	// 7. GET /api/v1/lessons/next with NO token -> 401. The route is protected
	// by default (it is absent from publicRoutes), and this is what proves it.
	rec = do(http.MethodGet, "/api/v1/lessons/next", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("next lesson: status = %d, want %v (body: %s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("next lesson no token: 'Content-Type' = %s, want 'application/problem+json'", rec.Header().Get("Content-Type"))
	}
}

func TestE2E_SubmitSessionMovesCompetency(t *testing.T) {
	pool, _, _, do := setupE2ETest(t)
	loginToken := userRegisterLogin(t, pool, do)

	// 3. GET /api/v1/progress WITH the token -> 200, body is the seeded competency JSON
	rec := do(http.MethodGet, "/api/v1/progress", "", loginToken.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("progress: status = %d, want %v (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		Keys map[string]struct {
			Score   float64 `json:"score"`
			Samples int     `json:"samples"`
		} `json:"keys"`
		Ngrams    map[string]any `json:"ngrams"`
		NgramTier int            `json:"ngram_tier"`
		TargetWPM int            `json:"target_wpm"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("progress: decode response: %v", err)
	}

	if got.NgramTier != 20 {
		t.Errorf("ngram_tier = %d, want 20", got.NgramTier)
	}
	if got.TargetWPM != 40 {
		t.Errorf("target_wpm = %d, want 40", got.TargetWPM)
	}
	if len(got.Keys) != 4 {
		t.Errorf("len(keys) = %d, want 4", len(got.Keys))
	}
	for _, k := range []string{"e", "t", "a", "o"} {
		if _, ok := got.Keys[k]; !ok {
			t.Errorf("keys is missing %q; got %v", k, slices.Sorted(maps.Keys(got.Keys)))
		}
	}
	if len(got.Ngrams) != 0 {
		t.Errorf("len(ngrams) = %d, want 0", len(got.Ngrams))
	}

	// 4. GET /api/v1/lessons/next with the token -> 200; every character of
	// every word is a key this user has already unlocked.
	rec = do(http.MethodGet, "/api/v1/lessons/next", "", loginToken.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("next lesson: status = %d, want %v (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("next lesson: 'Content-Type' = %s, want 'application/json'", rec.Header().Get("Content-Type"))
	}

	var lesson openapi.Lesson
	if err := json.NewDecoder(rec.Body).Decode(&lesson); err != nil {
		t.Fatalf("next lesson: decode response: %v", err)
	}

	// Non-nil, not merely non-empty: api/openapi.yaml declares both arrays
	// required, and a nil slice marshals as null rather than [].
	if lesson.Words == nil {
		t.Fatal("next lesson: words is null, want an array")
	}
	if lesson.Targets == nil {
		t.Error("next lesson: targets is null, want an array")
	}

	// The keys this submission will report on are taken from the lesson the
	// server just generated, not hardcoded. engine.ApplyResult ignores
	// observations for keys the user has not unlocked, so a fabricated key
	// that happened to be locked would move nothing and every assertion below
	// would pass against a document that never changed.
	seen := make(map[rune]bool)
	var lessonKeys []rune
	for _, word := range lesson.Words {
		for _, r := range word {
			if !seen[r] {
				seen[r] = true
				lessonKeys = append(lessonKeys, r)
			}
		}
	}
	if len(lessonKeys) == 0 {
		t.Fatal("lesson contained no characters, so there is nothing to submit")
	}

	// Every key gets the same observation, which is what makes the two derived
	// numbers below constants rather than functions of the key count: the count
	// cancels in both formulas.
	//
	//	accuracy = 1 - (k*5)/(k*20)                = 1 - 1/4      = 0.75
	//	wpm      = (k*20 / 5) / (k*3750 / 60000)   = 4k / (k/16)  = 64
	//
	// Both stay exact in float64 because each division lands on a
	// representable value: 20 is a multiple of 5, and 60000/3750 is 16, a power
	// of two. Numbers without that property drift - 16 attempts over 3840ms is
	// also 50 wpm algebraically, but truncates to 49 at seven keys.
	//
	// The lesson is generated from a fresh seed per request (Decision 7), so
	// how many distinct keys it uses is not knowable here. That is precisely
	// why the fixture is built to not care.
	const (
		attemptsPerKey = 20
		errorsPerKey   = 5
		millisPerKey   = 3750

		wantWPM      = 64
		wantAccuracy = 0.75
	)

	// Built from the generated request type rather than a format string, so a
	// change to the spec's SessionSubmission breaks this at compile time
	// instead of at run time. Ngrams is an empty map, not omitted: the schema
	// requires the member, and a nil map marshals as null.
	submission := openapi.SessionSubmission{
		Keys:   make(map[string]openapi.Observation, len(lessonKeys)),
		Ngrams: map[string]openapi.Observation{},
	}
	for _, r := range lessonKeys {
		submission.Keys[string(r)] = openapi.Observation{
			Attempts:    attemptsPerKey,
			Errors:      errorsPerKey,
			TotalMillis: millisPerKey,
		}
	}
	submissionBody, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("marshalling submission: %v", err)
	}

	// 5. POST /api/v1/sessions WITH the token -> 201 and the derived summary.
	// The route is protected, so the token is not optional here.
	rec = do(http.MethodPost, "/api/v1/sessions", string(submissionBody), loginToken.Token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit session: status = %d, want %v (body: %s)",
			rec.Code, http.StatusCreated, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("submit session: 'Content-Type' = %s, want 'application/json'",
			rec.Header().Get("Content-Type"))
	}

	var summary openapi.SessionSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("submit session: decode response: %v", err)
	}

	if summary.Id == uuid.Nil {
		t.Error("submit session: id is the nil UUID, so no row was inserted")
	}
	if summary.Wpm != wantWPM {
		t.Errorf("submit session: wpm = %d, want %d", summary.Wpm, wantWPM)
	}
	if summary.Accuracy != wantAccuracy {
		t.Errorf("submit session: accuracy = %v, want %v", summary.Accuracy, wantAccuracy)
	}
	if summary.CompletedAt.IsZero() {
		t.Error("submit session: completed_at is the zero time")
	}

	// 6. GET /api/v1/progress again -> the competency the submission moved.
	// This second read is the point of the whole test: everything above could
	// pass with the competency write silently dropped, because the response
	// summary is built from the session row alone.
	rec = do(http.MethodGet, "/api/v1/progress", "", loginToken.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("progress after: status = %d, want %v (body: %s)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var after struct {
		Keys map[string]struct {
			Score         float64   `json:"score"`
			Samples       int       `json:"samples"`
			LastPracticed time.Time `json:"last_practiced"`
		} `json:"keys"`
		Ngrams    map[string]any `json:"ngrams"`
		NgramTier int            `json:"ngram_tier"`
		TargetWPM int            `json:"target_wpm"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&after); err != nil {
		t.Fatalf("progress after: decode response: %v", err)
	}

	for _, r := range lessonKeys {
		k := string(r)
		item, ok := after.Keys[k]
		if !ok {
			t.Errorf("progress after: submitted key %q is missing; keys are %v",
				k, slices.Sorted(maps.Keys(after.Keys)))
			continue
		}

		// updateScore's contract is prev.Samples + o.Attempts, and every key
		// started at zero samples, so this is exact.
		if item.Samples != attemptsPerKey {
			t.Errorf("progress after: key %q samples = %d, want %d",
				k, item.Samples, attemptsPerKey)
		}
		if item.Score <= 0 {
			t.Errorf("progress after: key %q score = %v, want it to have moved off zero",
				k, item.Score)
		}

		// api/openapi.yaml promises completed_at is "the same instant the
		// engine records as last_practiced on every item in the submission".
		// This is the only test that reads both halves off the wire, which is
		// what makes it the one able to hold the spec to that sentence.
		if !item.LastPracticed.Equal(summary.CompletedAt) {
			t.Errorf("progress after: key %q last_practiced = %v, but the session "+
				"reported completed_at = %v; the spec promises one instant",
				k, item.LastPracticed, summary.CompletedAt)
		}
	}

	// Keys the submission did not mention keep their seeded zero values, which
	// is what proves ApplyResult folded the observations in rather than
	// rewriting the whole document.
	for k, item := range after.Keys {
		if seen[[]rune(k)[0]] {
			continue
		}
		if item.Samples != 0 || item.Score != 0 {
			t.Errorf("progress after: unsubmitted key %q was modified: %+v", k, item)
		}
	}

	// A score of 0.825 sits below unlockKeyThreshold (0.85), so this one
	// submission unlocks nothing, advances no tier and raises no target. That
	// makes the rest of the document a fixed point, and progression itself
	// stays where it is tested properly - internal/engine's property tests.
	if len(after.Keys) != len(got.Keys) {
		t.Errorf("progress after: len(keys) = %d, want %d unchanged",
			len(after.Keys), len(got.Keys))
	}
	if after.NgramTier != got.NgramTier {
		t.Errorf("progress after: ngram_tier = %d, want %d unchanged",
			after.NgramTier, got.NgramTier)
	}
	if after.TargetWPM != got.TargetWPM {
		t.Errorf("progress after: target_wpm = %d, want %d unchanged",
			after.TargetWPM, got.TargetWPM)
	}
}
