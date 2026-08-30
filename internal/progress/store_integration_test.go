package progress

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/corygyarmathy/typist/internal/platform/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool opens a pool against DATABASE_URL and migrates. Skips entirely
// when DATABASE_URL is unset, so local `make test` stays green.
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

	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("failed to apply database migrations: %v", err)
	}

	return pool
}

// createTestUserWithProgress inserts a users row and the user_progress row that
// registration would have written alongside it, returning the user's ID and
// deleting both when the test finishes.
//
// It does not register through auth: user_progress references users(id)
// directly, so a credential row would add an argon2id hash to a test that has
// nothing to do with authentication. The competency document is the same
// hand-written fixture the unit tests use, for the reason recorded on
// startingCompetency - derived from InitialCompetency it would agree with the
// code by construction.
//
// Cleanup is by primary key and cascades to user_progress.
func createTestUserWithProgress(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var id pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	userID := uuid.UUID(id.Bytes)

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup: deleting user %s: %v", userID, err)
		}
	})

	if err := newPgxRepository(pool).CreateUserProgress(
		ctx, userID, []byte(startingCompetency)); err != nil {
		t.Fatalf("creating user progress for %s: %v", userID, err)
	}

	return userID
}

// readCompetency reads the stored document back through the pool, outside any
// transaction the test is holding.
func readCompetency(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) engine.CompetencyState {
	t.Helper()

	raw, err := newPgxRepository(pool).GetUserProgress(context.Background(), userID)
	if err != nil {
		t.Fatalf("reading competency for %s: %v", userID, err)
	}
	var cs engine.CompetencyState
	if err := json.Unmarshal(raw, &cs); err != nil {
		t.Fatalf("unmarshalling competency for %s: %v", userID, err)
	}
	return cs
}

// TestStore_RoundTrip covers the seam internal/session will submit through: load
// the document under a row lock, change it, write it back, commit.
//
// The load half is the part that needs a real database rather than a fake. A
// fake Repository hands back bytes the test already has, so it never exercises
// the decode - which is where a state that silently arrives empty would come
// from, and an empty state folded through engine.ApplyResult and saved is a
// user's whole unlock set, tier and target erased inside a committed
// transaction, with no error raised anywhere.
func TestStore_RoundTrip(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	userID := createTestUserWithProgress(t, pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	defer func() {
		if cerr := tx.Rollback(ctx); cerr != nil && !errors.Is(cerr, pgx.ErrTxClosed) {
			t.Errorf("rolling back: %v", cerr)
		}
	}()

	store := NewStore(tx)

	loaded, err := store.LoadForUpdate(ctx, userID)
	if err != nil {
		t.Fatalf("LoadForUpdate: %v", err)
	}

	// The document that was stored, not a zero value. Asserting the fields
	// individually rather than only len(Keys) means a decode that half-works -
	// the KeyScores custom unmarshaller running but the scalars not, say - still
	// fails here.
	if len(loaded.Keys) != 4 {
		t.Errorf("loaded keys: got %d, want 4 (a zero CompetencyState means the decode was skipped)", len(loaded.Keys))
	}
	if _, ok := loaded.Keys['e']; !ok {
		t.Error("loaded keys: 'e' missing; KeyScores decodes by character, not code point")
	}
	if loaded.NgramTier != 20 {
		t.Errorf("loaded ngram tier: got %d, want 20", loaded.NgramTier)
	}
	if loaded.TargetWPM != 40 {
		t.Errorf("loaded target wpm: got %d, want 40", loaded.TargetWPM)
	}

	// Mutate the way engine.ApplyResult would: a key's score moves, and the
	// tool-managed target rises.
	practiced := time.Now().UTC()
	loaded.Keys['e'] = engine.ItemScore{Score: 0.5, Samples: 30, LastPracticed: practiced}
	loaded.TargetWPM = 45

	if err := store.Save(ctx, userID, loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}

	got := readCompetency(t, pool, userID)
	if got.TargetWPM != 45 {
		t.Errorf("persisted target wpm: got %d, want 45", got.TargetWPM)
	}
	e, ok := got.Keys['e']
	if !ok {
		t.Fatal("persisted keys: 'e' missing")
	}
	if e.Score != 0.5 {
		t.Errorf("persisted score: got %v, want 0.5", e.Score)
	}
	if e.Samples != 30 {
		t.Errorf("persisted samples: got %d, want 30", e.Samples)
	}
	// Equal, not ==: marshalling strips the monotonic clock reading, so a time
	// that has been through JSON compares unequal to the one it was built from.
	if !e.LastPracticed.Equal(practiced) {
		t.Errorf("persisted last_practiced: got %v, want %v", e.LastPracticed, practiced)
	}
	// The keys not in the submission must survive untouched. Save replaces the
	// whole document, so a partial write shows up as missing keys rather than as
	// a stale value.
	if len(got.Keys) != 4 {
		t.Errorf("persisted keys: got %d, want 4", len(got.Keys))
	}
}

// TestStore_Save_RolledBackLeavesDocumentUnchanged proves Store writes go
// through the transaction it was constructed with rather than around it.
//
// Without this, a Store that had somehow acquired its own pool connection would
// pass the round-trip test above and still break the submit path: the session
// row would roll back while the competency change stayed committed, which is the
// exact split the transaction exists to prevent.
func TestStore_Save_RolledBackLeavesDocumentUnchanged(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	userID := createTestUserWithProgress(t, pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}

	store := NewStore(tx)
	loaded, err := store.LoadForUpdate(ctx, userID)
	if err != nil {
		t.Fatalf("LoadForUpdate: %v", err)
	}

	loaded.TargetWPM = 999
	if err := store.Save(ctx, userID, loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	got := readCompetency(t, pool, userID)
	if got.TargetWPM != 40 {
		t.Errorf("target wpm after rollback: got %d, want 40 (the write escaped the transaction)", got.TargetWPM)
	}
}
