package engine

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

type harnessCorpus struct {
	keys   []rune           // KeyOrder: letters, most frequent first
	freq   map[rune]float64 // unigram frequency, sums to ~1
	ngrams []string         // NgramsByFrequency: built once, not per call
}

// Compile-time assertion that the fake satisfies the interface
var _ Corpus = harnessCorpus{}

func (h harnessCorpus) KeyOrder() []rune {
	return h.keys
}

func (h harnessCorpus) NgramsByFrequency() []string {
	return h.ngrams
}

// Transitions(ctx): ignores ctx and returns the same 26 Candidates (all
// keys), Freq = the letter's unigram frequency. An independence assumption
// — no context sensitivity at all.
func (h harnessCorpus) Transitions(context string) []Candidate {
	var cands []Candidate
	for _, k := range h.keys {
		cands = append(cands, Candidate{Char: k, Freq: h.freq[k]})
	}

	return cands
}

// The constructor:
//
//  1. One literal — the 26 letters in frequency order ("etaoinshrdlcumwfgypbvkjxqz")
//     plus their 26 frequencies. That's the entire hand-written input.
//  2. ngrams: two nested loops over order producing all 676 pairs, sorted
//     descending by freq[a]*freq[b]. Generated, not typed.
//
// Perf note, since you'll run ~500 lessons: build ngrams once in the
// constructor and return the stored slice. ApplyResult calls
// NgramsByFrequency() for its tier clamp on every lesson, and activeNgrams
// walks the tier prefix — rebuilding and re-sorting 676 strings per call turns
// a millisecond test into a slow one for no reason.
func newHarnessCorpus() harnessCorpus {
	freqOrder := "etaoinshrdlcumwfgypbvkjxqz"
	var keys []rune
	for _, r := range freqOrder {
		keys = append(keys, r)
	}

	// approximately correct, add up to ~1
	weights := []float64{
		0.127, // e
		0.091, // t
		0.082, // a
		0.075, // o
		0.070, // i
		0.067, // n
		0.063, // s
		0.061, // h
		0.060, // r
		0.043, // d
		0.040, // l
		0.028, // c
		0.028, // u
		0.024, // m
		0.023, // w
		0.022, // f
		0.020, // g
		0.020, // y
		0.019, // p
		0.015, // b
		0.010, // v
		0.008, // k
		0.002, // j
		0.002, // x
		0.001, // q
		0.001, // z
	}

	freq := make(map[rune]float64, len(keys))
	for i, r := range keys {
		freq[r] = weights[i]
	}

	// construct all possible pairs (bigrams)
	var ngrams []string
	prod := make(map[string]float64, len(keys)*len(keys))
	for _, outer := range keys {
		for _, inner := range keys {
			ng := string([]rune{outer, inner})
			ngrams = append(ngrams, ng)
			prod[ng] = freq[outer] * freq[inner]
		}
	}

	// Descending by frequency product: `prod[b]` first, not `prod[a]`.
	slices.SortFunc(ngrams, func(a, b string) int {
		return cmp.Compare(prod[b], prod[a])
	})

	return harnessCorpus{
		keys:   keys,
		freq:   freq,
		ngrams: ngrams,
	}
}

type profile struct {
	name     string
	accuracy float64 // P(first keystroke correct)
	meanMs   float64 // absolute mean interval between correct keystrokes
	jitter   float64 // fractional spread on meanMs
	errorMs  float64 // extra millis charged to a mistyped position
}

type keystroke struct {
	ms      float64
	errored bool
}

// simulate a lesson for a given profile, returning a Result (Observations of
// how the simulated student performed for the keys and ngrams).
func simulate(lesson Lesson, p profile, r *rand.Rand) Result {
	var res Result
	res.Keys = make(map[rune]Observation)
	res.Ngrams = make(map[string]Observation)
	var ks []keystroke

	for _, word := range lesson.Words {
		chars := []rune(word)

		// keys
		for i := range len(chars) {
			interval := p.meanMs + (r.Float64()*2-1)*p.jitter

			errors := res.Keys[chars[i]].Errors
			errored := r.Float64() > p.accuracy
			if errored {
				interval += p.errorMs
				errors += 1
			}

			attempts := res.Keys[chars[i]].Attempts + 1
			totalMS := res.Keys[chars[i]].TotalMillis + interval

			res.Keys[chars[i]] = Observation{
				Attempts:    attempts,
				Errors:      errors,
				TotalMillis: totalMS,
			}

			ks = append(ks, keystroke{ms: interval, errored: errored})
		}

		// ngrams
		for i := 1; i < len(chars); i++ {

			// form bi-gram
			g := string(chars[i-1 : i+1])

			attempts := res.Ngrams[g].Attempts + 1

			errors := res.Ngrams[g].Errors
			if ks[i-1].errored || ks[i].errored {
				errors += 1
			}

			totalMS := res.Ngrams[g].TotalMillis
			totalMS += ks[i-1].ms + ks[i].ms

			res.Ngrams[g] = Observation{
				Attempts:    attempts,
				Errors:      errors,
				TotalMillis: totalMS,
			}
		}

		// Reset the keystrokes slice per word
		ks = ks[:0]
	}

	return res
}

type stats struct {
	lessons        int          // lessons actually run
	fullAlphabetAt int          // first lesson where len(s.Keys) == len(c.KeyOrder()); -1 if never
	phaseFlipAt    int          // first lesson where phaseIsNgrams; -1 if never
	targetRaisedAt int          // last lesson where TargetWPM changed
	tierAdvancedAt int          // last lesson where NgramTier changed
	unlockedAt     map[rune]int // key -> the lesson index at which it entered state.Keys
}

// runLearner compares the state before and after each ApplyResult and stamps the index.
// That feeds all five assertions: (1) fullAlphabetAt bounded, (2) struggling's fullAlphabetAt > good's,
// (3) phaseFlipAt >= fullAlphabetAt and both ≥ 0, (4) lessons − targetRaisedAt > 100 plus the derived
// equilibrium on the returned state's TargetWPM, (5) t.Logf the returned NgramTier.
func runLearner(c Corpus, p profile, r *rand.Rand, maxLessons int) (state CompetencyState, st stats, finalNow time.Time) {
	// state: the learner's competency. Reassigned once per lesson from ApplyResult.
	state = InitialCompetency(c)

	// now: the harness clock. Advanced by each lesson's own duration so that
	// LastPracticed ages and decayedScore actually decays.
	now := testNow

	// st: the event log. -1 means "this never happened", which has to be set
	// explicitly because 0 is a real lesson number.
	st = stats{fullAlphabetAt: -1, phaseFlipAt: -1, targetRaisedAt: -1,
		tierAdvancedAt: -1, unlockedAt: make(map[rune]int)}

	for i := range maxLessons {
		lesson := NextLesson(state, c, now, r)
		if len(lesson.Words) == 0 {
			break // no seeds; the engine has nothing to offer
		}
		res := simulate(lesson, p, r)

		// Read the two fields that need a before/after comparison out of the
		// old state, while `state` still holds it.
		tierBefore := state.NgramTier
		targetBefore := state.TargetWPM

		// Advance the clock by how long the simulated typist took. res.Keys
		// covers every character exactly once; res.Ngrams would double-count,
		// because each middle character sits in two bigram windows.
		var lessonMillis float64
		for _, o := range res.Keys {
			lessonMillis += o.TotalMillis
		}
		now = now.Add(time.Duration(lessonMillis)*time.Millisecond + 30*time.Second)

		// From here on, `state` holds the new CompetencyState.
		state = ApplyResult(state, c, res, now)
		st.lessons = i + 1

		// record when each key was unlocked
		for k := range state.Keys {
			if _, seen := st.unlockedAt[k]; !seen {
				st.unlockedAt[k] = i
			}
		}

		// "First" events. The -1 guard makes the write happen on the earliest lesson
		// where the condition holds.
		if st.fullAlphabetAt == -1 && len(state.Keys) == len(c.KeyOrder()) {
			st.fullAlphabetAt = i
		}
		if st.phaseFlipAt == -1 && phaseIsNgrams(state, c, now) {
			st.phaseFlipAt = i
		}

		// "Last" events: these need the before values, because the question is
		// "did this field change during lesson i", and no guard, because a later
		// change should overwrite an earlier one.
		if state.NgramTier != tierBefore {
			st.tierAdvancedAt = i
		}
		if state.TargetWPM != targetBefore {
			st.targetRaisedAt = i
		}
	}

	return state, st, now
}

func TestHarness_GoodLearnerCompletesAlphabet(t *testing.T) {
	c := newHarnessCorpus()
	r := rand.New(rand.NewPCG(0, 12345))
	good := profile{name: "good", accuracy: 0.98, meanMs: 200, jitter: 50, errorMs: 200}

	const maxLessons = 200 // set to just over the expected lessons to unlock all keys
	state, st, _ := runLearner(c, good, r, maxLessons)

	if st.fullAlphabetAt < 0 {
		t.Fatalf("good learner never unlocked the full alphabet in %d lessons. %d keys unlocked.", st.lessons, len(state.Keys))
	}
	t.Logf("good learner: full alphabet at lesson %d", st.fullAlphabetAt)
}

func TestHarness_StrugglingLearnerTakesLonger(t *testing.T) {
	c := newHarnessCorpus()
	good := profile{name: "good", accuracy: 0.98, meanMs: 200, jitter: 50, errorMs: 200}
	struggling := profile{name: "struggling", accuracy: 0.93, meanMs: 375, jitter: 50, errorMs: 375}

	const maxLessonsGood = 200 // set to just over the expected lessons to unlock all keys
	const maxLessonsStr = 5000 // set to just over the expected lessons to unlock all keys

	// A fresh *rand.Rand seeded identically for each run, so the only
	// difference between the two runs is the profile.
	stateGood, stGood, finalNowGood := runLearner(c, good, rand.New(rand.NewPCG(0, 12345)), maxLessonsGood)
	stateStr, stStr, finalNowStr := runLearner(c, struggling, rand.New(rand.NewPCG(0, 12345)), maxLessonsStr)

	if stStr.fullAlphabetAt < 0 {
		t.Fatalf(
			"struggling learner never unlocked the full alphabet in %d lessons. %d keys unlocked.",
			stStr.lessons, len(stateStr.Keys),
		)
	}
	t.Logf("good learner: full alphabet at lesson %d", stGood.fullAlphabetAt)
	prev := 0
	for _, k := range c.KeyOrder() {
		at := stGood.unlockedAt[k]
		item := stateGood.Keys[k]
		t.Logf("%c unlocked at %4d (+%3d)  samples=%4d  score=%.3f  decayed=%.3f",
			k, at, at-prev, item.Samples, item.Score, decayedScore(item, finalNowGood))
		prev = at
	}
	t.Logf("good learner: final target WPM: %d", stateGood.TargetWPM)
	t.Logf("good learner: completed lessons: %d", stGood.lessons)

	t.Logf("struggling learner: full alphabet at lesson %d", stStr.fullAlphabetAt)
	prev = 0
	for _, k := range c.KeyOrder() {
		at := stStr.unlockedAt[k]
		item := stateStr.Keys[k]
		t.Logf("%c unlocked at %4d (+%3d)  samples=%4d  score=%.3f  decayed=%.3f",
			k, at, at-prev, item.Samples, item.Score, decayedScore(item, finalNowStr))
		prev = at
	}

	t.Logf("struggling learner: final target WPM: %d", stateStr.TargetWPM)
	t.Logf("struggling learner: completed lessons: %d", stStr.lessons)

	if stStr.fullAlphabetAt <= stGood.fullAlphabetAt {
		t.Errorf("struggling learner reached the full alphabet at lesson %d, "+
			"good learner at %d; want struggling strictly later",
			stStr.fullAlphabetAt, stGood.fullAlphabetAt)
	}
}

func TestHarness_PhaseFlipsAfterAlphabet(t *testing.T) {
	c := newHarnessCorpus()
	r := rand.New(rand.NewPCG(0, 12345))
	good := profile{name: "good", accuracy: 0.98, meanMs: 200, jitter: 50, errorMs: 200}

	state := InitialCompetency(c)
	if phaseIsNgrams(state, c, testNow) {
		t.Fatalf("expected initial phase to be keys, not ngrams")
	}

	const maxLessons = 200 // set to just over the expected lessons to unlock all keys
	_, st, _ := runLearner(c, good, r, maxLessons)

	t.Logf("phase flip lesson: %d", st.phaseFlipAt)

	if st.phaseFlipAt == -1 {
		t.Fatalf("expected phase to flip from keys to ngrams")
	}

	if st.phaseFlipAt < st.fullAlphabetAt {
		t.Fatalf(
			"expected phase to flip after all keys unlocked. Phase flip lesson %d, keys unlocked lesson %d",
			st.phaseFlipAt, st.fullAlphabetAt,
		)
	}
}

func TestHarness_TargetWPMStabilises(t *testing.T) {
	c := newHarnessCorpus()
	r := rand.New(rand.NewPCG(0, 12345))
	const accuracy = 0.98
	good := profile{name: "good", accuracy: accuracy, meanMs: 200, jitter: 50, errorMs: 200}

	const maxLessons = 200 // set to just over the expected lessons to unlock all keys
	state, _, _ := runLearner(c, good, r, maxLessons)

	t.Logf("target WPM after initial %d lessons: %d", maxLessons, state.TargetWPM)

	state, _, _ = runLearner(c, good, r, 100)

	t.Logf("target WPM after further %d lessons: %d", 100, state.TargetWPM)

	want := float64(state.TargetWPM) * wSpeed / (targetRaiseScore - wAccuracy*accuracy) // ≈ 110
	t.Logf("wanted WPM after final %d lessons: %v", 100, want)

	state, _, _ = runLearner(c, good, r, 100)
	t.Logf("target WPM after final %d lessons: %d", 100, state.TargetWPM)

	if (float64(state.TargetWPM) - want) > targetWPMStep {
		t.Fatalf("final target WPM outside expected range. Wanted %v, got %d", want, state.TargetWPM)
	}
}

func TestHarness_NgramTierAdvances(t *testing.T) {
	c := newHarnessCorpus()
	r := rand.New(rand.NewPCG(0, 12345))
	good := profile{name: "good", accuracy: 0.98, meanMs: 200, jitter: 50, errorMs: 200}

	const maxLessons = 200 // set to just over the expected lessons to unlock all keys

	state, st, _ := runLearner(c, good, r, maxLessons)

	t.Logf("total number of ngrams: %d", len(c.NgramsByFrequency()))
	t.Logf("ngram tier: %d -> %d (last advance at lesson %d of %d)",
		startingNgramTier, state.NgramTier, st.tierAdvancedAt, st.lessons)

	active := activeNgrams(state, c)
	for _, ng := range active {
		item := state.Ngrams[ng]
		t.Logf("%-3s samples=%4d score=%.3f", ng, item.Samples, item.Score)
	}

	if state.NgramTier <= startingNgramTier {
		t.Errorf("ngram tier never advanced: still %d after %d lessons",
			state.NgramTier, st.lessons)
	}
}

func TestHarness_Deterministic(t *testing.T) {
	c := newHarnessCorpus()
	r1 := rand.New(rand.NewPCG(0, 12345))
	r2 := rand.New(rand.NewPCG(0, 12345))
	good := profile{name: "good", accuracy: 0.98, meanMs: 200, jitter: 50, errorMs: 200}

	const maxLessons = 200 // set to just over the expected lessons to unlock all keys
	_, st1, _ := runLearner(c, good, r1, maxLessons)
	_, st2, _ := runLearner(c, good, r2, maxLessons)

	if st1.lessons != st2.lessons {
		t.Fatalf("different number of lessons ran. Expected %d, got %d.",
			st1.lessons, st2.lessons,
		)
	}

	if st1.fullAlphabetAt != st2.fullAlphabetAt {
		t.Fatalf("alphabet unlocked at different lessons. Expected %d, got %d.",
			st1.fullAlphabetAt, st2.fullAlphabetAt,
		)
	}

	if st1.phaseFlipAt != st2.phaseFlipAt {
		t.Fatalf("phase flipped at different lessons. Expected %d, got %d.",
			st1.phaseFlipAt, st2.phaseFlipAt,
		)
	}

	if st1.targetRaisedAt != st2.targetRaisedAt {
		t.Fatalf("WPM target raised lesson number was different. Expected %d, got %d.",
			st1.targetRaisedAt, st2.targetRaisedAt,
		)
	}

	if st1.tierAdvancedAt != st2.tierAdvancedAt {
		t.Fatalf("ngram tier advanced lesson number was different. Expected %d, got %d.",
			st1.tierAdvancedAt, st2.tierAdvancedAt,
		)
	}

	for _, k := range c.KeyOrder() {
		at1 := st1.unlockedAt[k]
		at2 := st2.unlockedAt[k]

		if at1 != at2 {
			t.Fatalf("key %c unlocked at different lesson. Wanted %d, got %d", k, at1, at2)
		}
	}
}
