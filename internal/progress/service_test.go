package progress

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/corpus"
	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/google/uuid"
)

// proves interface is implemented
var _ Repository = &fakeRepo{}

type fakeRepo struct {
	err error  // specify error to be 'returned'
	cs  []byte // competency state in JSON
}

func (f *fakeRepo) CreateUserProgress(ctx context.Context, userID uuid.UUID, competency []byte) (err error) {
	return nil
}

func (f *fakeRepo) GetUserProgress(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	return f.cs, f.err
}

// startingCompetency is the persisted document for a freshly-registered user:
// the four starting keys unlocked at zero score.
//
// It is hand-written rather than marshalled from engine.InitialCompetency
// deliberately. The fixture's job is to state the document shape docs/schema.md
// publishes, independently of the Go types that read it - derived from
// InitialCompetency it would agree with the code by construction and stop
// catching the case where a json tag and the stored document drift apart.
//
// The zero last_practiced is not a placeholder: engine.InitialCompetency seeds
// ItemScore{}, so this is exactly what registration writes. decayedScore clamps
// on it rather than dividing by an age of zero.
const startingCompetency = `{
  "keys": {
    "e": {"score": 0, "samples": 0, "last_practiced": "0001-01-01T00:00:00Z"},
    "t": {"score": 0, "samples": 0, "last_practiced": "0001-01-01T00:00:00Z"},
    "a": {"score": 0, "samples": 0, "last_practiced": "0001-01-01T00:00:00Z"},
    "o": {"score": 0, "samples": 0, "last_practiced": "0001-01-01T00:00:00Z"}
  },
  "ngrams": {},
  "ngram_tier": 20,
  "target_wpm": 40
}`

// noKeysCompetency is a structurally valid document with an empty unlock set.
// lessonScope.seeds() yields no candidates for it, so engine.NextLesson returns
// a bare engine.Lesson{} - the one input that exercises the ErrEmptyLesson
// guard.
const noKeysCompetency = `{"keys": {}, "ngrams": {}, "ngram_tier": 20, "target_wpm": 40}`

// The seed and the clock are the two injected sources of non-determinism.
// Pinning both is what makes an assertion on generated words meaningful.
const (
	seedHi uint64 = 0x9E3779B97F4A7C15
	seedLo uint64 = 0xBF58476D1CE4E5B9
)

var fixedNow = time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)

func newTestCorpus(t *testing.T) engine.Corpus {
	t.Helper()
	c, err := corpus.New()
	if err != nil {
		t.Fatalf("loading corpus: %v", err)
	}
	return c
}

// newTestService builds a Service by struct literal rather than through
// NewService, which is the whole point of keeping newRand and now as unexported
// fields (phase-4 plan, Decision 7): the test pins both without the constructor
// growing options for a single caller.
//
// pool stays nil on purpose. NextLesson reads only through repo, so a non-nil
// pool here would suggest a dependency that does not exist; if the field is
// ever dereferenced on this path, the panic is the correct outcome.
func newTestService(t *testing.T, repo Repository) *Service {
	t.Helper()
	return &Service{
		repo:    repo,
		corpus:  newTestCorpus(t),
		newRand: func() *rand.Rand { return rand.New(rand.NewPCG(seedHi, seedLo)) },
		now:     func() time.Time { return fixedNow },
	}
}

// TestNextLesson_FixedSeedIsDeterministic pins the injection seam, not the
// engine: engine_test.go already covers what NextLesson generates. What is new
// at this layer is that the Service hands the engine a fresh generator per call
// (Decision 7 - *rand.Rand is not safe for concurrent use), and this asserts
// that "fresh" still means "reproducible" when the seed is fixed.
//
// A regression that shared one generator across calls, or seeded it from the
// clock, fails here.
func TestNextLesson_FixedSeedIsDeterministic(t *testing.T) {
	svc := newTestService(t, &fakeRepo{cs: []byte(startingCompetency)})
	ctx := context.Background()
	userID := uuid.New()

	first, err := svc.NextLesson(ctx, userID)
	if err != nil {
		t.Fatalf("first NextLesson: unexpected error: %v", err)
	}
	second, err := svc.NextLesson(ctx, userID)
	if err != nil {
		t.Fatalf("second NextLesson: unexpected error: %v", err)
	}

	if !slices.Equal(first.Words, second.Words) {
		t.Errorf("words differ across calls with a fixed seed:\n first  = %v\n second = %v",
			first.Words, second.Words)
	}
	if !slices.Equal(first.Targets, second.Targets) {
		t.Errorf("targets differ across calls with a fixed seed:\n first  = %v\n second = %v",
			first.Targets, second.Targets)
	}

	// Both slices must be non-nil, or the handler serves `null` for two fields
	// api/openapi.yaml declares required arrays. The ErrEmptyLesson guard is
	// what keeps the nil case off this path; this is the other half of it.
	if first.Words == nil {
		t.Error("words is nil; it marshals as null against a required array")
	}
	if first.Targets == nil {
		t.Error("targets is nil; it marshals as null against a required array")
	}
}

// TestNextLesson_Failures covers every way the read path can fail. Each case is
// a 500 by design (see the sentinels in models.go): a registered user always has
// a competency row, so every one of them means the server is wrong, not the client.
func TestNextLesson_Failures(t *testing.T) {
	errBoom := errors.New("db is down")

	tests := map[string]struct {
		fake    *fakeRepo
		wantErr error // nil = any non-nil error is acceptable
	}{
		"repository failure propagates": {
			fake:    &fakeRepo{err: errBoom},
			wantErr: errBoom,
		},
		"empty unlock set is rejected, not served as an empty lesson": {
			fake:    &fakeRepo{cs: []byte(noKeysCompetency)},
			wantErr: ErrEmptyLesson,
		},
		"corrupt stored document": {
			fake: &fakeRepo{cs: []byte(`{"keys": not json`)},
		},
		// KeyScores.UnmarshalJSON rejects a multi-rune key. Without that check
		// "th" would decode as 't' and join the unlock set unearned, so the
		// failure belongs here rather than being silently repaired.
		"multi-rune key in the stored document": {
			fake: &fakeRepo{cs: []byte(`{"keys": {"th": {"score": 0, "samples": 0}}}`)},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			svc := newTestService(t, tc.fake)

			lesson, err := svc.NextLesson(context.Background(), uuid.New())
			if err == nil {
				t.Fatalf("NextLesson: got lesson %v and a nil error, want an error", lesson)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("NextLesson: error = %v, want it to wrap %v", err, tc.wantErr)
			}
			if len(lesson.Words) != 0 || len(lesson.Targets) != 0 {
				t.Errorf("NextLesson: returned a non-empty lesson alongside an error: %v", lesson)
			}
		})
	}
}
