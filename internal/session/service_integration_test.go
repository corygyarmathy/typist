package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These tests cover Submit's transaction: the ordering of the five steps inside
// it, and that a failure at any of them leaves the database untouched.
//
// The CompetencyStore is a fake rather than the real progress.Store, for two
// reasons. progress.Store's own round trip is already covered by
// progress.TestStore_RoundTrip, so exercising it again here would duplicate
// that test while testing nothing new about session. More importantly, session
// must never import progress - that is the whole point of declaring
// CompetencyStore here - and a test that reached for the concrete type would
// quietly establish the coupling the interface exists to prevent. The two
// halves meet for real in the end-to-end test, through the wiring that owns
// the adapter.
//
// The Corpus is likewise a hand-written fake, following the same rule engine
// itself follows (see the Candidate doc comment in engine/types.go).

var errBoom = errors.New("boom")

// fakeCorpus is the smallest Corpus that lets ApplyResult run: a short key
// order for the unlock check and a short ngram ranking for the tier check.
// Transitions is only used by the lesson generator, which Submit never calls.
type fakeCorpus struct{}

func (fakeCorpus) KeyOrder() []rune                      { return []rune{'e', 't', 'a', 'o', 'i'} }
func (fakeCorpus) NgramsByFrequency() []string           { return []string{"th", "he", "in"} }
func (fakeCorpus) Transitions(string) []engine.Candidate { return nil }

// fakeCompetencyStore records what Submit asked of it, and can be told to fail
// at either end. Every call to the factory returns the same value, so the load
// and the save are provably talking to one store rather than two.
type fakeCompetencyStore struct {
	state   engine.CompetencyState // returned by LoadForUpdate
	loadErr error
	saveErr error

	loads int
	saved *engine.CompetencyState // nil until Save is called

	// onSave runs after a successful Save, while the transaction is still
	// open. It exists so a test can sabotage the commit that follows.
	onSave func()
}

func (f *fakeCompetencyStore) LoadForUpdate(
	ctx context.Context,
	userID uuid.UUID,
) (engine.CompetencyState, error) {
	f.loads++
	if f.loadErr != nil {
		return engine.CompetencyState{}, f.loadErr
	}
	return f.state, nil
}

func (f *fakeCompetencyStore) Save(
	ctx context.Context,
	userID uuid.UUID,
	s engine.CompetencyState,
) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = &s
	if f.onSave != nil {
		f.onSave()
	}
	return nil
}

// startingState has 'e' already unlocked, because ApplyResult deliberately
// ignores observations for keys the user has not been given yet - a client
// cannot unlock a key by claiming to have practised it. Without 'e' present
// here the competency document would come back unchanged and the timestamp
// assertion below would have nothing to read.
func startingState(lastPracticed time.Time) engine.CompetencyState {
	return engine.CompetencyState{
		Keys: engine.KeyScores{
			'e': {Score: 0.5, Samples: 100, LastPracticed: lastPracticed},
		},
		Ngrams:    map[string]engine.ItemScore{},
		NgramTier: 2,
		TargetWPM: 40,
	}
}

// newTestService builds the real Service - real pool, real repository - with
// only the competency store and the clock swapped out. Going through
// NewService rather than a struct literal keeps the constructor's own wiring
// under test; a mismatch between the newRepo closure and the Repository
// interface would surface here rather than at first production traffic.
//
// The clock advances a second on every reading rather than returning a
// constant. That is what gives the single-clock assertions teeth: a Submit
// that called s.now() twice - once for ApplyResult and once for the session
// row - would still agree with itself under a frozen clock, and the bug this
// is guarding against would sail through. Callers assert against the FIRST
// reading, so a second call sends one of the two consumers a second into the
// future and the comparison fails.
func newTestService(t *testing.T, store CompetencyStore, now time.Time) *Service {
	t.Helper()
	svc := NewService(
		newTestPool(t),
		func(pgx.Tx) CompetencyStore { return store },
		fakeCorpus{},
	)
	tick := now
	svc.now = func() time.Time {
		reading := tick
		tick = tick.Add(time.Second)
		return reading
	}
	return svc
}

// countSessions reports how many session rows exist for a user. Used to assert
// that a rolled-back Submit wrote nothing.
func countSessions(t *testing.T, svc *Service, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := svc.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("counting sessions for %s: %v", userID, err)
	}
	return n
}

// TestSubmit_Integration_HappyPath walks the whole transaction and then reads
// the row back through a separate connection, which is what proves the commit
// actually happened rather than the assertions merely observing the returned
// struct.
func TestSubmit_Integration_HappyPath(t *testing.T) {
	// Deliberately ragged, carrying nanoseconds a real time.Now() would carry
	// and a timestamptz column cannot store. A whole-second fixture would make
	// Submit's Truncate a no-op and this test could not tell whether it was
	// there at all - which is how the two timestamps came to disagree on the
	// wire in the first place.
	now := time.Date(2026, 3, 1, 12, 0, 0, 352670493, time.UTC)

	// What Submit must store in both places: the reading at the resolution
	// timestamptz can actually hold. Every assertion below compares against
	// this rather than against now, so dropping the truncation fails the test.
	wantAt := now.Truncate(time.Microsecond)

	lastYear := now.AddDate(-1, 0, 0)

	store := &fakeCompetencyStore{state: startingState(lastYear)}
	svc := newTestService(t, store, now)
	ctx := context.Background()
	userID := createTestUser(t, svc.pool)

	// 250 keystrokes, no errors, over exactly one minute: wpm 50, accuracy 1.
	res := engine.Result{
		Keys: map[rune]engine.Observation{
			'e': {Attempts: 250, Errors: 0, TotalMillis: 60000},
		},
	}

	sess, err := svc.Submit(ctx, userID, res)
	if err != nil {
		t.Fatalf("Submit: unexpected error: %v", err)
	}

	if sess.ID == uuid.Nil {
		t.Error("Submit: returned a session with the nil UUID")
	}
	if sess.WPM != 50 {
		t.Errorf("Submit: wpm got %d, want 50", sess.WPM)
	}
	if sess.Accuracy != 1 {
		t.Errorf("Submit: accuracy got %v, want 1", sess.Accuracy)
	}

	// Read the committed row back. accuracy is NUMERIC; the float64 round trip
	// has its own test in TestCreateSession_NumericRoundTrip, so cast in SQL
	// here and keep this test about the transaction.
	var (
		gotWPM         int32
		gotAccuracy    float64
		gotCompletedAt time.Time
	)
	if err := svc.pool.QueryRow(ctx,
		`SELECT wpm, accuracy::float8, completed_at FROM sessions WHERE id = $1`,
		sess.ID).Scan(&gotWPM, &gotAccuracy, &gotCompletedAt); err != nil {
		t.Fatalf("reading back session %s: %v", sess.ID, err)
	}
	if gotWPM != 50 || gotAccuracy != 1 {
		t.Errorf("committed row: wpm %d accuracy %v, want 50 and 1", gotWPM, gotAccuracy)
	}

	if store.loads != 1 {
		t.Errorf("LoadForUpdate called %d times, want exactly 1", store.loads)
	}
	if store.saved == nil {
		t.Fatal("Save was never called, so the competency update was silently dropped")
	}

	// ApplyResult must have run on what LoadForUpdate returned, not on a zero
	// state: 'e' keeps its samples and gains this submission's attempts.
	got := store.saved.Keys['e']
	if got.Samples != 350 {
		t.Errorf("saved samples for 'e': got %d, want 350 (100 existing + 250 submitted)", got.Samples)
	}

	// The single-clock rule, and the truncation that makes it observable.
	// One s.now() reading feeds both ApplyResult and the session row, but only
	// the session row goes through a column that rounds - so the reading has to
	// be truncated before it is split, or the competency document keeps 493
	// nanoseconds the row cannot and the two disagree despite describing the
	// same instant. Equal rather than ==, because the value out of Postgres
	// carries the session's location.
	if !got.LastPracticed.Equal(wantAt) {
		t.Errorf("saved LastPracticed for 'e': got %v, want %v", got.LastPracticed, wantAt)
	}
	if !gotCompletedAt.Equal(wantAt) {
		t.Errorf("committed completed_at: got %v, want %v", gotCompletedAt, wantAt)
	}
	if !gotCompletedAt.Equal(got.LastPracticed) {
		t.Errorf("the session row and the competency document disagree about when "+
			"this submission happened: %v vs %v", gotCompletedAt, got.LastPracticed)
	}
}

// TestSubmit_Integration_RollsBackOnSaveFailure is the test that would have
// caught the discarded Save error: without the error check, Save fails, the
// transaction commits anyway, and the session row below exists.
func TestSubmit_Integration_RollsBackOnSaveFailure(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 352670493, time.UTC)
	store := &fakeCompetencyStore{
		state:   startingState(now.AddDate(-1, 0, 0)),
		saveErr: errBoom,
	}
	svc := newTestService(t, store, now)
	ctx := context.Background()
	userID := createTestUser(t, svc.pool)

	res := engine.Result{
		Keys: map[rune]engine.Observation{
			'e': {Attempts: 250, Errors: 0, TotalMillis: 60000},
		},
	}

	sess, err := svc.Submit(ctx, userID, res)
	if err == nil {
		t.Fatalf("Submit: expected an error, got session %+v", sess)
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("Submit: error does not wrap the store failure: %v", err)
	}
	if sess != (Session{}) {
		t.Errorf("Submit: returned a non-zero session alongside an error: %+v", sess)
	}

	// The session insert and the competency save share one transaction, so a
	// failed save must take the session row with it.
	if n := countSessions(t, svc, userID); n != 0 {
		t.Errorf("expected 0 sessions after rollback, got %d", n)
	}
}

// TestSubmit_Integration_RollsBackOnLoadFailure covers the discarded
// LoadForUpdate error, which is the more dangerous of the two: unchecked, the
// zero CompetencyState flows into ApplyResult, which has nothing to update, and
// the empty result is then written over the user's real document.
func TestSubmit_Integration_RollsBackOnLoadFailure(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 352670493, time.UTC)
	store := &fakeCompetencyStore{loadErr: errBoom}
	svc := newTestService(t, store, now)
	ctx := context.Background()
	userID := createTestUser(t, svc.pool)

	res := engine.Result{
		Keys: map[rune]engine.Observation{
			'e': {Attempts: 250, Errors: 0, TotalMillis: 60000},
		},
	}

	if _, err := svc.Submit(ctx, userID, res); !errors.Is(err, errBoom) {
		t.Fatalf("Submit: expected the store failure, got %v", err)
	}
	if store.saved != nil {
		t.Errorf("Save ran after LoadForUpdate failed, writing %+v over the "+
			"user's real competency document", *store.saved)
	}
	if n := countSessions(t, svc, userID); n != 0 {
		t.Errorf("expected 0 sessions after rollback, got %d", n)
	}
}

// TestSubmit_Integration_UnknownUser pins what happens when the FK is violated:
// the CreateSession insert is the failing step, and its error must surface
// rather than being discarded into a zero-valued success.
//
// Asserting merely that some error came back is not enough. A violated
// constraint puts the whole transaction into a failed state, so the commit at
// the end fails too - which means dropping the CreateSession error check
// entirely still produces an error, just a later and far less informative one.
// Matching the SQLSTATE is what separates the two.
func TestSubmit_Integration_UnknownUser(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 352670493, time.UTC)
	store := &fakeCompetencyStore{state: startingState(now.AddDate(-1, 0, 0))}
	svc := newTestService(t, store, now)
	ctx := context.Background()

	res := engine.Result{
		Keys: map[rune]engine.Observation{
			'e': {Attempts: 250, Errors: 0, TotalMillis: 60000},
		},
	}

	sess, err := svc.Submit(ctx, uuid.New(), res)
	if err == nil {
		t.Fatalf("Submit: expected a foreign key violation, got session %+v", sess)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.ForeignKeyViolation {
		t.Errorf("Submit: want the foreign key violation to reach the caller, got %v", err)
	}
	if sess != (Session{}) {
		t.Errorf("Submit: returned a non-zero session alongside an error: %+v", sess)
	}
}

// TestSubmit_Integration_CommitFailure covers the branch nothing else reaches.
//
// A discarded tx.Commit error is the worst of the four the transaction can
// produce: the deferred rollback fires, nothing is written, and Submit hands
// back a fully populated Session with a nil error - so the client is told 201
// Created about a row that does not exist. Every other step here fails loudly;
// this one fails silently, which is exactly why it needs its own test.
//
// The commit is sabotaged by cancelling the request context from inside Save,
// after the session insert and the competency write have both succeeded. That
// is the narrow window where only the commit can still go wrong.
func TestSubmit_Integration_CommitFailure(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 352670493, time.UTC)
	store := &fakeCompetencyStore{state: startingState(now.AddDate(-1, 0, 0))}
	svc := newTestService(t, store, now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.onSave = cancel

	// A separate background context, so the user survives the cancellation and
	// its cleanup can still run.
	userID := createTestUser(t, svc.pool)

	res := engine.Result{
		Keys: map[rune]engine.Observation{
			'e': {Attempts: 250, Errors: 0, TotalMillis: 60000},
		},
	}

	sess, err := svc.Submit(ctx, userID, res)
	if err == nil {
		t.Fatalf("Submit: commit failed but a session was returned as success: %+v", sess)
	}
	if sess != (Session{}) {
		t.Errorf("Submit: returned a non-zero session alongside an error: %+v", sess)
	}
	if store.saved == nil {
		t.Error("Save never ran, so this test did not reach the commit at all")
	}
	if n := countSessions(t, svc, userID); n != 0 {
		t.Errorf("expected 0 sessions after a failed commit, got %d", n)
	}
}
