package session

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/google/uuid"
)

// TestDerive pins the two numbers the client never sends and the server is
// therefore solely responsible for (Decision 2).
//
// Every fixture below is chosen so the expected accuracy is exactly
// representable in binary floating point (errors/attempts is 1/8 or 0), which
// is why the assertions compare exactly rather than within a tolerance. An
// epsilon here would keep passing on an arithmetic mistake smaller than itself.
func TestDerive(t *testing.T) {
	cases := []struct {
		name         string
		res          engine.Result
		wantWPM      int32
		wantAccuracy float64
	}{
		{
			// 250 keystrokes, no errors, over exactly one minute.
			// wpm = (250/5) / (60000/60000) = 50
			name: "single key, perfect",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 250, Errors: 0, TotalMillis: 60000},
				},
			},
			wantWPM:      50,
			wantAccuracy: 1,
		},
		{
			// 256 keystrokes, 32 errors -> 32/256 = 0.125 exactly.
			// wpm = (256/5) / 1 = 51.2
			name: "several keys, with errors",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 128, Errors: 16, TotalMillis: 30000},
					't': {Attempts: 128, Errors: 16, TotalMillis: 30000},
				},
			},
			wantWPM:      51,
			wantAccuracy: 0.875,
		},
		{
			// The guard on Decision 2's trap. Identical Keys to the case above,
			// plus a deliberately enormous Ngrams map. A bigram's TotalMillis
			// covers two keystrokes and a middle character sits in two bigram
			// windows, so summing the ngram side double-counts both time and
			// attempts. The answer must not move.
			name: "ngrams are never summed",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 128, Errors: 16, TotalMillis: 30000},
					't': {Attempts: 128, Errors: 16, TotalMillis: 30000},
				},
				Ngrams: map[string]engine.Observation{
					"th": {Attempts: 9000, Errors: 4500, TotalMillis: 900000},
					"he": {Attempts: 9000, Errors: 4500, TotalMillis: 900000},
				},
			},
			wantWPM:      51,
			wantAccuracy: 0.875,
		},
		{
			// 259/5 = 51.8. Truncates to 51; rounding would give 52. The column
			// is INT, so which of the two happens is a decision, not an accident.
			name: "wpm truncates rather than rounds",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 259, Errors: 0, TotalMillis: 60000},
				},
			},
			wantWPM:      51,
			wantAccuracy: 1,
		},
		{
			// Slower than one minute: 128 keystrokes over two minutes.
			// wpm = (128/5) / (120000/60000) = 25.6/2 = 12.8 -> 12
			name: "sub-minute rate does not floor to zero",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 128, Errors: 0, TotalMillis: 120000},
				},
			},
			wantWPM:      12,
			wantAccuracy: 1,
		},
		{
			// Every attempt wrong. The repository's numericValue accepts 0, and
			// the column's CHECK allows it.
			name: "total failure",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 64, Errors: 64, TotalMillis: 60000},
				},
			},
			wantWPM:      12, // (64/5)/1 = 12.8
			wantAccuracy: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wpm, accuracy, err := derive(tc.res)
			if err != nil {
				t.Fatalf("derive: unexpected error: %v", err)
			}
			if wpm != tc.wantWPM {
				t.Errorf("wpm: got %d, want %d", wpm, tc.wantWPM)
			}
			if accuracy != tc.wantAccuracy {
				t.Errorf("accuracy: got %v, want %v", accuracy, tc.wantAccuracy)
			}
		})
	}
}

// TestDerive_Invalid covers the inputs that would otherwise produce a NaN or a
// division by zero.
//
// Decision 3's validation rejects all of these before derive is reached, so
// this is defence in depth rather than the primary guard. It earns its place
// because derive is where the bad number would actually be produced: a NaN
// accuracy survives all the way to the repository, where it surfaces as an
// opaque NUMERIC conversion failure that names nothing useful.
func TestDerive_Invalid(t *testing.T) {
	cases := []struct {
		name string
		res  engine.Result
	}{
		{
			name: "nil keys map",
			res:  engine.Result{},
		},
		{
			name: "empty keys map",
			res:  engine.Result{Keys: map[rune]engine.Observation{}},
		},
		{
			// Ngrams alone cannot carry a session: they are never summed, so
			// this is indistinguishable from an empty submission.
			name: "ngrams only",
			res: engine.Result{
				Keys:   map[rune]engine.Observation{},
				Ngrams: map[string]engine.Observation{"th": {Attempts: 10, TotalMillis: 2000}},
			},
		},
		{
			name: "zero elapsed time",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 10, Errors: 0, TotalMillis: 0},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wpm, accuracy, err := derive(tc.res)
			if err == nil {
				t.Fatalf("derive: expected an error, got wpm=%d accuracy=%v", wpm, accuracy)
			}
			// A caller that ignores the error must not be handed a NaN to
			// persist; the zero values are at least a valid session row.
			if math.IsNaN(accuracy) || math.IsInf(accuracy, 0) {
				t.Errorf("accuracy on the error path is %v, want a real number", accuracy)
			}
		})
	}
}

// TestValidate pins the submissions Decision 3 accepts.
//
// The item-count case sits exactly on maxSubmittedItems rather than safely
// under it: the cap is a `>` comparison, and a boundary fixture is the only
// thing that tells `>` from `>=`.
func TestValidate(t *testing.T) {
	atLimit := make(map[rune]engine.Observation, maxSubmittedItems)
	for i := range maxSubmittedItems {
		atLimit[rune(0x100+i)] = engine.Observation{Attempts: 1, TotalMillis: 100}
	}

	cases := []struct {
		name string
		res  engine.Result
	}{
		{
			name: "single perfect key",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 10, Errors: 0, TotalMillis: 2000},
				},
			},
		},
		{
			// Errors equal to attempts is a total failure, not a malformed
			// submission: every keystroke was wrong, which is a real thing a
			// beginner does. The rule is `errors > attempts`.
			name: "every attempt wrong",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 10, Errors: 10, TotalMillis: 2000},
				},
			},
		},
		{
			// Ngrams are optional; a lesson below the ngram tier produces none.
			name: "keys and ngrams together",
			res: engine.Result{
				Keys: map[rune]engine.Observation{
					'e': {Attempts: 10, Errors: 1, TotalMillis: 2000},
					't': {Attempts: 8, Errors: 0, TotalMillis: 1600},
				},
				Ngrams: map[string]engine.Observation{
					"th": {Attempts: 4, Errors: 1, TotalMillis: 900},
				},
			},
		},
		{
			name: "exactly the item limit",
			res:  engine.Result{Keys: atLimit},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validate(tc.res); err != nil {
				t.Errorf("validate: unexpected error: %v", err)
			}
		})
	}
}

// TestValidate_Rejects covers Decision 3's rejection rules.
//
// Every case also asserts errors.Is(err, ErrInvalidObservation). That is not
// redundant with "an error was returned": the sentinel is the only thing the
// handler has to distinguish a client's malformed submission (400) from a
// server fault (500), so an unwrapped rejection is a 500 on valid-shaped input.
func TestValidate_Rejects(t *testing.T) {
	overLimit := make(map[rune]engine.Observation, maxSubmittedItems+1)
	for i := range maxSubmittedItems + 1 {
		// Individually valid, so the cap is the only rule that can reject this.
		overLimit[rune(0x100+i)] = engine.Observation{Attempts: 1, TotalMillis: 100}
	}

	// Ngram cases need a valid key beside them: an empty Keys map is rejected
	// first and the ngram loop would never run.
	validKey := map[rune]engine.Observation{'e': {Attempts: 10, TotalMillis: 2000}}

	cases := []struct {
		name string
		res  engine.Result
	}{
		{
			name: "nil keys map",
			res:  engine.Result{},
		},
		{
			name: "empty keys map",
			res:  engine.Result{Keys: map[rune]engine.Observation{}},
		},
		{
			// Ngrams alone cannot carry a session: derive never sums them, so
			// this is indistinguishable from an empty submission.
			name: "ngrams only",
			res: engine.Result{
				Ngrams: map[string]engine.Observation{"th": {Attempts: 4, TotalMillis: 900}},
			},
		},
		{
			name: "one item over the limit",
			res:  engine.Result{Keys: overLimit},
		},
		{
			name: "key with zero attempts",
			res: engine.Result{
				Keys: map[rune]engine.Observation{'e': {Attempts: 0, TotalMillis: 2000}},
			},
		},
		{
			name: "key with negative attempts",
			res: engine.Result{
				Keys: map[rune]engine.Observation{'e': {Attempts: -1, TotalMillis: 2000}},
			},
		},
		{
			name: "key with more errors than attempts",
			res: engine.Result{
				Keys: map[rune]engine.Observation{'e': {Attempts: 10, Errors: 11, TotalMillis: 2000}},
			},
		},
		{
			// Reachable despite the errors > attempts rule above: -1 is not
			// greater than 10, so only the explicit negative check catches it.
			// It would otherwise make accuracy exceed 1 and trip the column's
			// CHECK constraint deep inside the transaction.
			name: "key with negative errors",
			res: engine.Result{
				Keys: map[rune]engine.Observation{'e': {Attempts: 10, Errors: -1, TotalMillis: 2000}},
			},
		},
		{
			name: "key with zero elapsed time",
			res: engine.Result{
				Keys: map[rune]engine.Observation{'e': {Attempts: 10, TotalMillis: 0}},
			},
		},
		{
			name: "key with negative elapsed time",
			res: engine.Result{
				Keys: map[rune]engine.Observation{'e': {Attempts: 10, TotalMillis: -1}},
			},
		},
		{
			// The four cases below prove the ngram loop runs at all. Deleting
			// it leaves every keys-only case above still passing.
			name: "ngram with zero attempts",
			res: engine.Result{
				Keys:   validKey,
				Ngrams: map[string]engine.Observation{"th": {Attempts: 0, TotalMillis: 900}},
			},
		},
		{
			name: "ngram with more errors than attempts",
			res: engine.Result{
				Keys:   validKey,
				Ngrams: map[string]engine.Observation{"th": {Attempts: 4, Errors: 5, TotalMillis: 900}},
			},
		},
		{
			name: "ngram with negative errors",
			res: engine.Result{
				Keys:   validKey,
				Ngrams: map[string]engine.Observation{"th": {Attempts: 4, Errors: -1, TotalMillis: 900}},
			},
		},
		{
			name: "ngram with zero elapsed time",
			res: engine.Result{
				Keys:   validKey,
				Ngrams: map[string]engine.Observation{"th": {Attempts: 4, TotalMillis: 0}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(tc.res)
			if err == nil {
				t.Fatal("validate: expected an error, got nil")
			}
			if !errors.Is(err, ErrInvalidObservation) {
				t.Errorf("validate: error does not wrap ErrInvalidObservation, "+
					"so the handler will map it to 500 rather than 400: %v", err)
			}
		})
	}
}

// TestSubmit_RejectsBeforeTransaction proves Submit's guard clauses return
// before any database work is attempted.
//
// The Service is zero-valued, so pool is nil and newProgress is nil. Reaching
// pool.Begin therefore fails the test loudly rather than silently opening a
// transaction the assertions could not see. This mirrors
// auth.TestRegister_GuardClauses, which relies on the same nil pool.
//
// Only validate's rejections are exercised here, because derive's error path is
// unreachable from Submit: validate guarantees a non-empty Keys map in which
// every observation has Attempts >= 1 and TotalMillis > 0, so chars and millis
// are both positive by the time derive runs. That is why derive's own guards
// are documented as defence in depth rather than as live error handling.
func TestSubmit_RejectsBeforeTransaction(t *testing.T) {
	cases := []struct {
		name string
		res  engine.Result
	}{
		{
			name: "no keys",
			res:  engine.Result{},
		},
		{
			name: "malformed observation",
			res: engine.Result{
				Keys: map[rune]engine.Observation{'e': {Attempts: 10, Errors: 11, TotalMillis: 2000}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{}

			sess, err := s.Submit(context.Background(), uuid.New(), tc.res)
			if err == nil {
				t.Fatalf("Submit: expected an error, got session %+v", sess)
			}
			if !errors.Is(err, ErrInvalidObservation) {
				t.Errorf("Submit: error does not wrap ErrInvalidObservation: %v", err)
			}
			if sess != (Session{}) {
				t.Errorf("Submit: returned a non-zero session alongside an error: %+v", sess)
			}
		})
	}
}
