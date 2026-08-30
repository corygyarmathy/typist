package session

import (
	"fmt"

	"github.com/corygyarmathy/typist/internal/engine"
)

// TODO(phase-4): business logic. The service depends on the Repository
// interface, never on the concrete implementation. Transactions are
// orchestrated here, not in handlers or repositories.

// - validation per Decision 3 — attempts < 1, errors > attempts, errors < 0, total_millis <= 0,
// empty keys, and the maxSubmittedItems cardinality cap.

const (
	// charsPerWord is the standard five-characters-per-word convention for WPM.
	// engine.go carries the same number unexported for its own target-speed
	// arithmetic; the two must agree, or the WPM this reports on the session row
	// diverges from the WPM the engine scored the submission against.
	charsPerWord = 5.0
	// msPerMin converts the observations' milliseconds into minutes.
	msPerMin = 60000.0
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
func derive(res engine.Result) (wpm int, accuracy float64, err error) {
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
	wpm = int((float64(chars) / charsPerWord) / (millis / msPerMin))

	return wpm, accuracy, nil
}
