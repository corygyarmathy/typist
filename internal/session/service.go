package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Business logic. The service depends on the Repository
// interface, never on the concrete implementation. Transactions are
// orchestrated here, not in handlers or repositories.

const (
	// charsPerWord is the standard five-characters-per-word convention for WPM.
	// engine.go carries the same number unexported for its own target-speed
	// arithmetic; the two must agree, or the WPM this reports on the session row
	// diverges from the WPM the engine scored the submission against.
	charsPerWord = 5.0
	// msPerMin converts the observations' milliseconds into minutes.
	msPerMin = 60000.0

	// maxSubmittedItems bounds how many distinct observations one submission may
	// carry. router.go caps the request body at 1 MiB, which bounds bytes, not
	// map cardinality - a million single-rune keys is a small body. The corpus
	// is the real ceiling: data/corpus.json defines 26 letters and 499 bigrams,
	// so a legitimate submission holds at most 525 distinct items. This sits at
	// roughly twice that, leaving room for the corpus to grow without ever
	// rejecting honest input.
	maxSubmittedItems = 1000
)

// derive computes the session summary from the submitted observations.
// chars, errs and millis all sum over res.Keys.
//
// Never sum res.Ngrams into these totals. A bigram's TotalMillis covers two
// keystrokes and a middle character sits in two bigram windows, so the ngram
// side double-counts both time and attempts - the same double-count the phase-3
// harness hit, as a production bug rather than a test one.
//
// Word-boundary spaces are not scored as keys (docs/engine.md), so chars is
// the lesson length excluding spaces and the WPM is very slightly conservative
// against the five-characters-per-word convention. And wpm truncates rather
// than rounds, because the column is INT.
func derive(res engine.Result) (wpm int32, accuracy float64, err error) {
	var chars, errs int
	var millis float64
	for _, o := range res.Keys {
		chars += o.Attempts
		errs += o.Errors
		// Accumulated as float64: truncating each key's TotalMillis to an
		// integer would shed up to a millisecond per key.
		millis += o.TotalMillis
	}

	// Validation rejects both of these before derive is reached, so this is
	// defence in depth. chars == 0 makes accuracy a NaN, which stays a NaN all
	// the way to the repository and surfaces as an opaque NUMERIC conversion
	// failure naming nothing useful.
	if chars == 0 {
		return 0, 0, fmt.Errorf(
			"%w: no key observations to derive a summary from",
			ErrInvalidObservation,
		)
	}
	if millis <= 0 {
		return 0, 0, fmt.Errorf(
			"%w: total elapsed time is not positive",
			ErrInvalidObservation,
		)
	}

	accuracy = 1 - float64(errs)/float64(chars)
	// Float division throughout, truncating once at the end. Dividing the ints
	// first floors each operand independently, and millis/60000 in particular
	// floors to zero for anything under a minute.
	wpm = int32((float64(chars) / charsPerWord) / (millis / msPerMin))

	return wpm, accuracy, nil
}

// validate validates all result observations, returning nil on ok and
// ErrInvalidObservation when invalid.
func validate(res engine.Result) error {
	if len(res.Keys) == 0 {
		return fmt.Errorf("%w: result has no keys", ErrInvalidObservation)
	}

	if n := len(res.Keys) + len(res.Ngrams); n > maxSubmittedItems {
		return fmt.Errorf("%w: %d items submitted, limit is %d", ErrInvalidObservation, n, maxSubmittedItems)
	}

	for r, o := range res.Keys {
		if err := validateObservation(o); err != nil {
			return fmt.Errorf("key %q: %w", r, err)
		}
	}

	for s, o := range res.Ngrams {
		if err := validateObservation(o); err != nil {
			return fmt.Errorf("ngram %q: %w", s, err)
		}
	}

	// valid
	return nil
}

// validateObservation confirms all observed attempts, errors, and totalmillis
// are valid, returning nil on ok and ErrInvalidObservation when invalid.
func validateObservation(o engine.Observation) error {
	if o.Attempts < 1 {
		return fmt.Errorf("%w: attempts less than 1", ErrInvalidObservation)
	}
	if o.Errors > o.Attempts {
		return fmt.Errorf("%w: errors greater than attempts", ErrInvalidObservation)
	}
	if o.Errors < 0 {
		return fmt.Errorf("%w: errors less than 0", ErrInvalidObservation)
	}
	if o.TotalMillis <= 0 {
		return fmt.Errorf("%w: TotalMillis less than or equal to 0", ErrInvalidObservation)
	}
	// valid
	return nil
}

type CompetencyStore interface {
	LoadForUpdate(ctx context.Context, userID uuid.UUID) (engine.CompetencyState, error)
	Save(ctx context.Context, userID uuid.UUID, s engine.CompetencyState) error
}

type Service struct {
	pool        *pgxpool.Pool
	newRepo     func(pgx.Tx) Repository
	newProgress func(pgx.Tx) CompetencyStore
	corpus      engine.Corpus
	now         func() time.Time
}

func NewService(
	pool *pgxpool.Pool,
	newProgress func(tx pgx.Tx) CompetencyStore,
	corpus engine.Corpus,
) *Service {
	return &Service{
		pool:        pool,
		newRepo:     func(tx pgx.Tx) Repository { return newPgxRepository(tx) },
		newProgress: newProgress,
		corpus:      corpus,
		now:         time.Now,
	}
}

func (s *Service) Submit(ctx context.Context, userID uuid.UUID, res engine.Result) (Session, error) {
	if err := validate(res); err != nil {
		return Session{}, fmt.Errorf("validating result: %w", err)
	}

	wpm, acc, err := derive(res)
	if err != nil {
		return Session{}, fmt.Errorf("deriving result: %w", err)
	}

	// Truncated to the resolution timestamptz can actually hold. One reading
	// feeds both consumers, but only the session row goes through a
	// column that rounds; without this the competency document keeps 493
	// nanoseconds the row cannot, and the two disagree on the wire despite
	// describing the same instant.
	now := s.now().Truncate(time.Microsecond)

	// Begin db tx -> load -> apply -> insert session -> save -> commit
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("beginning tx: %w", err)
	}
	defer func() {
		if cerr := tx.Rollback(ctx); cerr != nil && !errors.Is(cerr, pgx.ErrTxClosed) {
			slog.Error("rolling back database transaction", "cerr", cerr)
		}
	}()

	store := s.newProgress(tx)

	state, err := store.LoadForUpdate(ctx, userID)
	if err != nil {
		return Session{}, fmt.Errorf("loading competency: %w", err)
	}

	next := engine.ApplyResult(state, s.corpus, res, now)

	sess, err := s.newRepo(tx).CreateSession(ctx, userID, wpm, acc, now)
	if err != nil {
		return Session{}, fmt.Errorf("creating session: %w", err)
	}

	if err := store.Save(ctx, userID, next); err != nil {
		return Session{}, fmt.Errorf("saving competency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("committing tx: %w", err)
	}

	return sess, nil
}
