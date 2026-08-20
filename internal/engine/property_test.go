package engine

import (
	"math/rand/v2"
	"testing"
	"time"
)

// Property tests: invariants asserted over many generated states rather than
// the handful a table can enumerate.
//
// These deliberately do not use testing/quick. That package is frozen ("not
// accepting new features", per its own doc), and more to the point it cannot
// generate a *valid* CompetencyState: reflection-built maps would hold
// arbitrary runes and an unlocked set that is not a prefix of KeyOrder, which
// is a state the engine is never handed and whose failures would say nothing.
// Supplying quick.Generator to fix that means writing the generator by hand
// anyway - at which point a seeded loop is the same work, reproducible by
// construction, and readable without knowing the package.

const propertyIterations = 200

// randomValidState builds a CompetencyState the engine could actually be
// handed: the unlocked set is a PREFIX of c.KeyOrder(), because that is the
// invariant nextKeyToUnlock relies on (it returns the first key in KeyOrder
// not present in Keys, so a non-prefix set silently breaks "unlock the next
// most frequent key"). Scores, samples, recency, tier and target vary freely.
func randomValidState(c Corpus, r *rand.Rand, now time.Time) CompetencyState {
	order := c.KeyOrder()

	// At least startingKeys unlocked, at most the whole alphabet - the range a
	// real learner moves through.
	n := startingKeys + r.IntN(len(order)-startingKeys+1)

	keys := make(map[rune]ItemScore, n)
	for _, k := range order[:n] {
		keys[k] = ItemScore{
			Score:         r.Float64(),
			Samples:       r.IntN(500),
			LastPracticed: now.Add(-time.Duration(r.IntN(14*24)) * time.Hour),
		}
	}

	ngrams := make(map[string]ItemScore)
	all := c.NgramsByFrequency()
	tier := r.IntN(len(all) + 1)
	for _, g := range all[:tier] {
		if r.IntN(2) == 0 {
			continue // leave some in-scope ngrams unpractised
		}
		ngrams[g] = ItemScore{
			Score:         r.Float64(),
			Samples:       r.IntN(200),
			LastPracticed: now.Add(-time.Duration(r.IntN(14*24)) * time.Hour),
		}
	}

	return CompetencyState{
		Keys:      keys,
		Ngrams:    ngrams,
		NgramTier: tier,
		TargetWPM: startingTargetWPM + r.IntN(120),
	}
}

// TestNextLesson_ContainsOnlyUnlockedKeys is the invariant the roadmap names
// as the canonical property for this phase: whatever the state, a generated
// lesson may only use keys the learner has unlocked.
//
// It is meaningful specifically because harnessCorpus.Transitions ignores its
// context and offers all 26 letters for every step. The corpus therefore
// proposes locked keys constantly, and the only thing keeping them out of the
// lesson is the filter in candidates(). Delete that filter and this fails;
// a corpus that only ever proposed unlocked keys would let it pass vacuously.
func TestNextLesson_ContainsOnlyUnlockedKeys(t *testing.T) {
	c := newHarnessCorpus()

	for i := range propertyIterations {
		seed := uint64(i)
		r := rand.New(rand.NewPCG(seed, seed))
		state := randomValidState(c, r, testNow)

		lesson := NextLesson(state, c, testNow, r)

		for _, word := range lesson.Words {
			for _, got := range word {
				if _, ok := state.Keys[got]; !ok {
					t.Fatalf(
						"seed %d: lesson contains locked key %q in word %q; unlocked set was %q (%d keys), words %q",
						seed, got, word, keysOf(state), len(state.Keys), lesson.Words,
					)
				}
			}
		}
	}
}

// TestNextLesson_WordShape pins the generator's output shape across the same
// spread of states. Words may be SHORTER than minWordLen: the walk ends a word
// early when every candidate is locked (step 5, decision 4), which is accepted
// behaviour rather than a defect - so the assertion is an upper bound plus
// non-emptiness, not a range.
func TestNextLesson_WordShape(t *testing.T) {
	c := newHarnessCorpus()

	for i := range propertyIterations {
		seed := uint64(i)
		r := rand.New(rand.NewPCG(seed, seed))
		state := randomValidState(c, r, testNow)

		lesson := NextLesson(state, c, testNow, r)

		if len(lesson.Words) != lessonWords {
			t.Fatalf("seed %d: len(Words) = %d, want %d", seed, len(lesson.Words), lessonWords)
		}

		for _, word := range lesson.Words {
			n := len([]rune(word))
			if n == 0 {
				t.Fatalf("seed %d: empty word in %q", seed, lesson.Words)
			}
			if n > maxWordLen {
				t.Fatalf("seed %d: word %q is %d runes, want at most %d", seed, word, n, maxWordLen)
			}
		}
	}
}

// TestNextLesson_TargetsAreUnlockedOrInScope guards the telemetry field
// against naming something the learner cannot yet be practising: a target key
// must be unlocked, and a target ngram must have all its keys unlocked.
func TestNextLesson_TargetsAreUnlockedOrInScope(t *testing.T) {
	c := newHarnessCorpus()

	for i := range propertyIterations {
		seed := uint64(i)
		r := rand.New(rand.NewPCG(seed, seed))
		state := randomValidState(c, r, testNow)

		lesson := NextLesson(state, c, testNow, r)

		for _, target := range lesson.Targets {
			for _, got := range target {
				if _, ok := state.Keys[got]; !ok {
					t.Fatalf(
						"seed %d: target %q references locked key %q; unlocked set was %q",
						seed, target, got, keysOf(state),
					)
				}
			}
		}
	}
}

// keysOf renders the unlocked set in KeyOrder-independent form for failure
// messages. Sorted so two failures on the same state read identically.
func keysOf(s CompetencyState) string {
	out := make([]rune, 0, len(s.Keys))
	for k := range s.Keys {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return string(out)
}
