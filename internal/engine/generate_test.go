package engine

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// weightOf returns the weight candidates() assigned to char, failing the test if
// char was filtered out entirely.
func weightOf(t *testing.T, cands []weightedCandidate, char rune) float64 {
	t.Helper()
	for _, wc := range cands {
		if wc.char == char {
			return wc.weight
		}
	}
	t.Fatalf("no candidate %q in %v", char, cands)
	return 0
}

func TestNeed_Practiced(t *testing.T) {
	s, _, _ := engineFixtures(testNow)

	n := need("th", s, testNow)
	if n == 1.0 {
		t.Errorf("practiced ngram need expected != 1, got %v", n)
	}
}

func TestNeed_Unpracticed(t *testing.T) {
	s, _, _ := engineFixtures(testNow)

	n := need("x", s, testNow)
	if n != 1.0 {
		t.Errorf("unpracticed key need expected == 1, got %v", n)
	}
}

func TestNeed_Decay(t *testing.T) {
	s, _, _ := engineFixtures(testNow)

	oldNeed := need("th", s, testNow)
	newNeed := need("th", s, testNow.AddDate(0, 0, 30))

	if oldNeed >= newNeed {
		t.Errorf("expected old < new, got: %v >= %v", oldNeed, newNeed)
	}
}

func TestWeightedSample_SingleCandidateReturned(t *testing.T) {
	char := 't'
	wc := make([]weightedCandidate, 1)
	wc[0] = weightedCandidate{char: char, weight: 0.8}

	r := rand.New(rand.NewPCG(0, 12345))
	s, ok := weightedSample(wc, r)
	if !ok || s != 't' {
		t.Errorf("expected 't', true (ok). Got: '%c', %v.", s, ok)
	}
}

func TestWeightedSample_ProportionalSamples(t *testing.T) {
	lightWeight := 0.3
	heavyWeight := 0.7
	charT := 't'
	charE := 'e'
	wc := make([]weightedCandidate, 2)
	wc[0] = weightedCandidate{char: charT, weight: lightWeight}
	wc[1] = weightedCandidate{char: charE, weight: heavyWeight}

	r := rand.New(rand.NewPCG(0, 12345))
	var canTCount float64
	var canECount float64
	sampleCount := 10000
	for range sampleCount {
		s, ok := weightedSample(wc, r)
		if !ok {
			t.Fatalf("unexpected empty return")
		}
		if s == charT {
			canTCount++
		} else {
			canECount++
		}
	}

	ratio := canTCount / canECount
	expectedRatio := lightWeight / heavyWeight
	tolerance := 0.05
	if math.Abs(expectedRatio-ratio) > tolerance {
		t.Errorf(
			"sample ratio outside %v tolerance. Expected %v, got %v. 't' count: %v. 'e' count: %v",
			tolerance, expectedRatio, ratio, canTCount, canECount,
		)
	}
}

func TestWeightedSample_EmptyWeightNotSelected(t *testing.T) {
	lightWeight := 0.0
	heavyWeight := 1.0
	charT := 't'
	charE := 'e'
	wc := make([]weightedCandidate, 2)
	wc[0] = weightedCandidate{char: charT, weight: lightWeight}
	wc[1] = weightedCandidate{char: charE, weight: heavyWeight}

	r := rand.New(rand.NewPCG(0, 12345))
	var canTCount float64
	var canECount float64
	sampleCount := 10
	for range sampleCount {
		s, ok := weightedSample(wc, r)
		if !ok {
			t.Errorf("unexpected empty return")
		}
		if s == charT {
			canTCount++
		} else {
			canECount++
		}
	}

	if canTCount != 0 {
		t.Errorf(
			"candidate %c w/ 0 weight unexpectedly selected. Count: %v",
			charT, canTCount)
	}
}

func TestWeightedSample_Empty(t *testing.T) {
	var wc []weightedCandidate

	r := rand.New(rand.NewPCG(0, 12345))
	_, ok := weightedSample(wc, r)
	if ok {
		t.Errorf("expected false (not ok). Got: %v.", ok)
	}
}

func TestWeightedSample_EmptyWeightNotOk(t *testing.T) {
	charT := 't'
	charE := 'e'
	wc := make([]weightedCandidate, 2)
	wc[0] = weightedCandidate{char: charT, weight: 0.0}
	wc[1] = weightedCandidate{char: charE, weight: 0.0}

	r := rand.New(rand.NewPCG(0, 12345))
	_, ok := weightedSample(wc, r)
	if ok {
		t.Errorf("expected false (not ok). Got: %v.", ok)
	}
}

func TestPick_ZeroDraw(t *testing.T) {
	charT := 't'
	charE := 'e'
	wc := make([]weightedCandidate, 2)
	wc[0] = weightedCandidate{char: charT, weight: 0.0}
	wc[1] = weightedCandidate{char: charE, weight: 1.0}

	draw := 0.0
	s, ok := pick(wc, draw)

	if s != charE || !ok {
		t.Errorf("Expected: sample %c, ok %v. Got: sample %c, ok %v",
			charE, true, s, ok)
	}
}

func TestPick_InternalBoundary(t *testing.T) {
	charT := 't'
	charE := 'e'
	wc := make([]weightedCandidate, 2)
	wc[0] = weightedCandidate{char: charT, weight: 1.0}
	wc[1] = weightedCandidate{char: charE, weight: 1.0}

	draw := 1.0
	s, ok := pick(wc, draw)

	if s != charE || !ok {
		t.Errorf("Expected: sample %c, ok %v. Got: sample %c, ok %v",
			charE, true, s, ok)
	}
}

func TestPick_FallThrough(t *testing.T) {
	char := 't'
	wc := make([]weightedCandidate, 1)
	wc[0] = weightedCandidate{char: char, weight: 1.0}

	draw := 1.0
	s, ok := pick(wc, draw)

	if s != char || !ok {
		t.Errorf("Expected: sample %c, ok %v. Got: sample %c, ok %v",
			char, true, s, ok)
	}
}

func TestPick_Empty(t *testing.T) {
	var wc []weightedCandidate

	draw := 1.0
	_, ok := pick(wc, draw)
	if ok {
		t.Errorf("expected false (not ok). Got: %v.", ok)
	}
}

func TestCandidates_WeakButRarerBeatsStrongButCommon(t *testing.T) {
	s, c, _ := engineFixtures(testNow)
	s.Keys['o'] = ItemScore{Score: 0.1, Samples: minSamples, LastPracticed: testNow}
	s.Keys['e'] = ItemScore{Score: 0.9, Samples: minSamples, LastPracticed: testNow}
	sc := newLessonScope(s, c, testNow)
	cands := sc.candidates("t")

	got := weightOf(t, cands, 'o') / weightOf(t, cands, 'e')

	// Both ngrams ("to", "te") are out of tier, so both ngram factors are 1.0
	// and only the key factor separates the two candidates. Derived from the
	// constant rather than hard-coded so a lambdaKey retune doesn't break it.
	want := (0.2 * (1 + lambdaKey*0.9)) / (0.3 * (1 + lambdaKey*0.1))

	if got <= 1 {
		t.Errorf("weak-but-rarer 'o' should outweigh strong-but-common 'e'; ratio %v", got)
	}
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("weight ratio: want %v, got %v", want, got)
	}
}

func TestCandidates_NgramFactorGatedOnScope(t *testing.T) {
	s, c, _ := engineFixtures(testNow)
	s.Keys['h'] = ItemScore{Score: 0.5, Samples: minSamples, LastPracticed: testNow}
	s.Keys['o'] = ItemScore{Score: 0.5, Samples: minSamples, LastPracticed: testNow} // equal key factors
	s.Ngrams["th"] = ItemScore{Score: 1.0, Samples: minSamples, LastPracticed: testNow}
	sc := newLessonScope(s, c, testNow)
	cands := sc.candidates("t")

	got := weightOf(t, cands, 'h') / weightOf(t, cands, 'o')

	// 'h' forms "th": active, and mastered at zero age, so its ngram factor is
	// exactly 1.0. 'o' forms "to", which is not in the corpus ngram list at all
	// -> gated out, so its factor is 1.0 too. With the key factors equal, only
	// the base frequencies are left.
	want := 0.5 / 0.2 // Freq('h') / Freq('o') after "t"

	// Ungated, "to" would score need 1.0 and take a 1 + lambdaNgramKeyPhase
	// factor of 1.5, dragging this ratio down to ~1.667.
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("ngram factor not gated on scope: want ratio %v, got %v", want, got)
	}
}

func TestNewLessonScope_PhasePicksRightLambda(t *testing.T) {
	s, c, _ := engineFixtures(testNow) // 'z','q','x' still locked
	sc1 := newLessonScope(s, c, testNow)
	if sc1.lambdaN != lambdaNgramKeyPhase {
		t.Errorf(
			"expected lambda for key phase %v, got %v",
			lambdaNgramKeyPhase, sc1.lambdaN,
		)
	}

	s, c, _ = engineKeysUnlocked(testNow)
	s.Keys = keysScoreFrom("theour", 0.9, minSamples, testNow) // mean 0.9 >= phaseThreshold
	sc2 := newLessonScope(s, c, testNow)

	if sc2.lambdaN != lambdaNgramNgramPhase {
		t.Errorf(
			"expected lambda for ngram phase %v, got %v",
			lambdaNgramNgramPhase, sc2.lambdaN,
		)
	}
}

func TestNextLesson_Targets(t *testing.T) {
	s, c, _ := engineFixtures(testNow)
	s.Keys['z'] = ItemScore{}

	r := rand.New(rand.NewPCG(0, 12345))
	lesson := NextLesson(s, c, testNow, r)

	// length check
	if len(lesson.Targets) != lessonTargets {
		t.Errorf("expected %v targets, got %v", lessonTargets, len(lesson.Targets))
	}

	// expected output check
	if lesson.Targets[0] != "z" {
		t.Errorf("expected to target freshly-unlocked 'z', got %v", lesson.Targets[0])
	}

	// dererministic check
	for range 10 {
		next := NextLesson(s, c, testNow, r)
		if !slices.Equal(lesson.Targets, next.Targets) {
			t.Errorf(
				"lesson targets unexpectedly unequal w/ same input.\nExpecting: %v\nGot:       %v",
				lesson.Targets, next.Targets,
			)
		}
	}
}

// Early game: fewer active items than lessonTargets, so targets() returns all
// of them rather than slicing past the end.
func TestNextLesson_TargetsFewerThanCap(t *testing.T) {
	s, c, _ := engineFixtures(testNow)
	s.Keys = keysFrom("th") // 2 unlocked keys
	s.NgramTier = 0         // and no active ngrams

	r := rand.New(rand.NewPCG(0, 12345))
	lesson := NextLesson(s, c, testNow, r)

	if len(lesson.Targets) != 2 {
		t.Errorf("expected all 2 active items as targets, got %v: %v",
			len(lesson.Targets), lesson.Targets)
	}
}
