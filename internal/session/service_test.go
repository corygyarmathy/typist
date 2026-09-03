package session

import (
	"math"
	"testing"

	"github.com/corygyarmathy/typist/internal/engine"
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
