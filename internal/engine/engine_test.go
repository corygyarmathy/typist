package engine

import (
	"maps"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
	"unicode/utf8"
)

// The engine is the most heavily-tested package in the codebase
// because it contains the genuinely interesting domain logic and it's
// trivially testable (pure functions).
//
// Test categories to build out:
//
//  1. Selection invariants
//     - Generated lessons only contain unlocked keys
//     - Weak keys appear more often than strong keys
//     - New keys are introduced when all current keys exceed threshold
//
//  2. Scoring invariants
//     - Accuracy is weighted more heavily than speed
//     - A perfect-accuracy slow run scores higher than fast-with-errors
//     - Scores decay over time (revisit weak items)
//
//  3. End-to-end progression
//     - Simulated user with consistent accuracy unlocks all 26 keys in
//     a bounded number of lessons
//     - Engine transitions from key-focus to ngram-focus around the
//     expected competency threshold
//
// Use testing/quick or property-based testing for the invariants.

// engineFixturesFor builds the one state/corpus/result triple every
// ApplyResult test works from. Callers vary a single axis - keyOrder - so the
// arithmetic below lives in exactly one place and cannot drift between
// fixtures when the constants are retuned.
//
// Every unlocked key and active ngram starts just below its gate but inside
// the band a single lesson can close, and res clears both gates:
//
//	score = alpha*instant + (1-alpha)*prev.Score
//
// so one step moves at most alpha of the way to instant, and the best score
// reachable in one call is alpha + (1-alpha)*prev = 0.3 + 0.7*prev. Crossing
// unlockKeyThreshold therefore needs
//
//	prev >= (unlockKeyThreshold - alpha) / (1 - alpha) ≈ 0.786
//
// which is why the prior score is 0.8: below the 0.85 bar, above the 0.786
// reachable floor. From there the gate needs
//
//	0.3*instant + 0.7(0.80) >= 0.85  ->  instant  >= 0.967
//	0.7*accuracy + 0.3      >= 0.967 ->  accuracy >= 0.952
//
// so res observes 50 attempts with 1 error (accuracy 0.98, and fast enough to
// clamp speed to 1.0), landing every item at 0.8558 with 99 samples - clear of
// both unlockKeyThreshold and minSamples. Halve the error budget or lower the
// prior score and the fixture becomes unsatisfiable by construction.
func engineFixturesFor(now time.Time, keyOrder []rune) (s CompetencyState, c fakeCorpus, res Result) {
	practised := now.AddDate(0, 0, -1) // y,m,d - stale enough that decay is live

	s = CompetencyState{
		Keys:      keysScoreFrom("theour", 0.8, minSamples-1, practised),
		Ngrams:    ngramsScoreFrom([]string{"th", "or", "eo", "ur"}, 0.8, minSamples-1, practised),
		NgramTier: 4,
		TargetWPM: 40,
	}

	c = fakeCorpus{
		keyOrder:          keyOrder,
		ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
		transitions: map[string][]Candidate{
			"t": {
				{Char: 'h', Freq: 0.5},
				{Char: 'e', Freq: 0.3},
				{Char: 'o', Freq: 0.2},
			},
			"h": {
				{Char: 'e', Freq: 0.6},
				{Char: 'o', Freq: 0.3},
				{Char: 'u', Freq: 0.1},
			},
			"e": {
				{Char: 'r', Freq: 0.4},
				{Char: 'o', Freq: 0.3},
				{Char: 't', Freq: 0.2},
				{Char: 'z', Freq: 0.1},
			},
			"o": {
				{Char: 'u', Freq: 0.5},
				{Char: 'r', Freq: 0.3},
				{Char: 't', Freq: 0.2},
			},
			"r": {
				{Char: 'e', Freq: 0.5},
				{Char: 'o', Freq: 0.3},
				{Char: 't', Freq: 0.1},
				{Char: 'q', Freq: 0.1},
			},
			"u": {
				{Char: 'z', Freq: 0.6},
				{Char: 'x', Freq: 0.4},
			},
		},
	}

	res = Result{
		Keys:   keysObservationFrom(s.Keys, 50, 1, 2000), // 60 WPM
		Ngrams: ngramsObservationFrom(s.Ngrams, 50, 1, 2000),
	}

	// Adversarial by default: an out-of-scope key and ngram in every payload,
	// simulating a buggy or hostile client. Neither is in s, so both must be
	// ignored - presence in the result must never create an item.
	res.Keys['x'] = Observation{Attempts: 10, Errors: 0, TotalMillis: 100}
	res.Ngrams["ou"] = Observation{Attempts: 10, Errors: 0, TotalMillis: 100}

	return s, c, res
}

// engineFixtures is the mid-progression case: 'z', 'q' and 'x' are in the
// corpus but not yet in s.Keys, so nextKeyToUnlock has something to find.
func engineFixtures(now time.Time) (s CompetencyState, c fakeCorpus, res Result) {
	return engineFixturesFor(now, []rune{'t', 'h', 'e', 'o', 'u', 'r', 'z', 'q', 'x'})
}

// engineKeysUnlocked is the alphabet-complete case: keyOrder is exactly the
// unlocked set, so nothing unlocks and no fresh zero-score key dilutes
// meanKeyDecayedScore - which is what makes the target-WPM raise reachable.
func engineKeysUnlocked(now time.Time) (s CompetencyState, c fakeCorpus, res Result) {
	return engineFixturesFor(now, []rune{'t', 'h', 'e', 'o', 'u', 'r'})
}

// confirm original still equals clone
func assertStatesEqual(t *testing.T, got CompetencyState, want CompetencyState) {
	t.Helper()
	if !maps.Equal(got.Keys, want.Keys) {
		t.Errorf(
			"Key field doesn't match.\nPrev: %v\nNext: %v",
			got.Keys, want.Keys,
		)
	}
	if !maps.Equal(got.Ngrams, want.Ngrams) {
		t.Errorf(
			"Ngrams field doesn't match.\nPrev: %v\nNext: %v",
			got.Ngrams, want.Ngrams,
		)
	}
	if got.NgramTier != want.NgramTier {
		t.Errorf(
			"NgramTier field doesn't match. Prev: %v Next: %v",
			got.NgramTier, want.NgramTier,
		)
	}
	if got.TargetWPM != want.TargetWPM {
		t.Errorf(
			"TargetWPM field doesn't match. Prev: %v Next: %v",
			got.TargetWPM, want.TargetWPM,
		)
	}
}

func TestApplyResult_Purity(t *testing.T) {
	s, c, res := engineFixtures(testNow)

	clone := s // copies NgramTier, TargetWPM by value
	clone.Keys = maps.Clone(s.Keys)
	clone.Ngrams = maps.Clone(s.Ngrams)

	assertStatesEqual(t, s, clone) // test pre-mutation

	next := ApplyResult(s, c, res, testNow)

	// mutate returned object's state
	next.TargetWPM += 10
	next.NgramTier += 10
	next.Keys['t'] = ItemScore{Score: 0.123} // writes through the map header
	next.Ngrams["th"] = ItemScore{Score: 0.123}

	assertStatesEqual(t, s, clone) // test post-mutation
}

// Same input twice -> equal outputs.
func TestApplyResult_Determinism(t *testing.T) {
	s, c, res := engineFixtures(testNow)

	next1 := ApplyResult(s, c, res, testNow)
	next2 := ApplyResult(s, c, res, testNow)

	assertStatesEqual(t, next1, next2)
}

func TestApplyResult_CorrectUnlocks(t *testing.T) {
	s, c, res := engineFixtures(testNow)
	next := ApplyResult(s, c, res, testNow)

	// check key unlocked correctly
	if len(s.Keys)+1 != len(next.Keys) {
		t.Errorf(
			"expected 1 unlocked key, got: %v",
			len(next.Keys)-len(s.Keys),
		)
	}
	unlockKey, ok := next.Keys['z']
	if !ok {
		t.Fatalf("key 'z' not unlocked when expected.")
	}
	if unlockKey.Score != 0 || unlockKey.Samples != 0 || !unlockKey.LastPracticed.IsZero() {
		t.Errorf(
			"unlocked key 'z' unexpectedly not in nil state. %v score, %v samples, %v last practiced.",
			unlockKey.Score, unlockKey.Samples, unlockKey.LastPracticed,
		)
	}

	// check ngram tier advanced correctly
	if s.NgramTier+1 != next.NgramTier {
		t.Errorf(
			"expected ngram tier to advance by 1, got: %v",
			next.NgramTier-s.NgramTier,
		)
	}

	// check target WPM did not advance
	if s.TargetWPM != next.TargetWPM {
		t.Errorf(
			"expected TargetWPM to stay, increased by: %v",
			next.TargetWPM-s.TargetWPM,
		)
	}
}

func TestApplyResult_LockedObservationIgnored(t *testing.T) {
	s, c, res := engineFixtures(testNow)
	next := ApplyResult(s, c, res, testNow)

	if _, ok := next.Keys['x']; ok {
		t.Errorf("key 'x' unexpectedly unlocked.")
	}

	if _, ok := next.Ngrams["ou"]; ok {
		t.Errorf("ngram 'ou' unexpectedly unlocked.")
	}
}

func TestApplyResult_DegenerateObservationDropped(t *testing.T) {
	s, c, res := engineFixtures(testNow)

	res.Keys['t'] = Observation{Attempts: 0}

	next := ApplyResult(s, c, res, testNow)

	if s.Keys['t'] != next.Keys['t'] {
		t.Errorf("degenerate observation for key 't' was unexpectedly accepted.")
	}
	if next.Keys['h'] == s.Keys['h'] {
		t.Errorf("sibling key 'h' should have folded despite 't' being degenerate")
	}
}

func TestApplyResult_TargetWPMIncreased(t *testing.T) {
	s, c, res := engineKeysUnlocked(testNow)
	next := ApplyResult(s, c, res, testNow)

	if len(next.Keys) != len(s.Keys) {
		t.Errorf("a new key was unexpectedly unlocked.")
	}

	// check target WPM advances
	if s.TargetWPM+targetWPMStep != next.TargetWPM {
		t.Errorf(
			"expected TargetWPM to advance by %v, increased by: %v",
			targetWPMStep, next.TargetWPM-s.TargetWPM,
		)
	}
}

func TestApplyResult_ZeroStateDoesntPanic(t *testing.T) {
	_, c, _ := engineKeysUnlocked(testNow)
	ApplyResult(CompetencyState{}, c, Result{}, testNow)
}

func TestLessonWords_WalkFollowsGraph(t *testing.T) {
	r := rand.New(rand.NewPCG(0, 12345))
	s, c, _ := engineFixtures(testNow)

	lesson := NextLesson(s, c, testNow, r)

	for _, word := range lesson.Words {
		chars := []rune(word)

		for i := range len(chars) - 1 {
			prev := chars[i]
			next := chars[i+1]
			cands := c.Transitions(string(prev))
			appears := slices.ContainsFunc(cands, func(c Candidate) bool {
				return c.Char == next
			})
			if !appears {
				t.Fatalf(
					"lesson words did not follow graph. word '%v', char pos '%v' rune pair '%c,%c', candidates: %v",
					word, i+1, prev, next, cands)
			}
		}
	}
}

func TestNextLesson_OnlyUnlockedKeys(t *testing.T) {
	r := rand.New(rand.NewPCG(0, 12345))
	s, c, _ := engineFixtures(testNow)

	lesson := NextLesson(s, c, testNow, r)

	for _, word := range lesson.Words {
		for _, k := range word {

			if _, ok := s.Keys[k]; !ok {
				t.Fatalf(
					"lesson generated w/ locked key. Key '%c', Word '%s', Lesson: '%v'",
					k, word, lesson,
				)
			}
		}
	}
}

func TestNextLesson_WithinBounds(t *testing.T) {
	r := rand.New(rand.NewPCG(0, 12345))
	s, c, _ := engineFixtures(testNow)

	lesson := NextLesson(s, c, testNow, r)

	if len(lesson.Words) != lessonWords {
		t.Fatalf(
			"Lesson generated w/ incorrect number of words. Expected %v, got %v",
			lessonWords, len(lesson.Words),
		)
	}

	for _, word := range lesson.Words {
		charLen := utf8.RuneCountInString(word)
		if charLen < 1 || charLen > maxWordLen {
			t.Fatalf(
				"Lesson generated word w/ incorrect length. Expected min %v, max %v, got %v",
				minWordLen, maxWordLen, charLen,
			)
		}
	}
}

func TestNextLesson_Determinism(t *testing.T) {
	r1 := rand.New(rand.NewPCG(0, 12345))
	r2 := rand.New(rand.NewPCG(0, 12345))
	s, c, _ := engineFixtures(testNow)

	l1 := NextLesson(s, c, testNow, r1)
	l2 := NextLesson(s, c, testNow, r2)

	if !slices.Equal(l1.Words, l2.Words) {
		t.Fatalf(
			"lessons generated w/ same random seed should match - didn't.\nLesson 1: %v\nLesson 2: %v",
			l1, l2,
		)
	}

}
