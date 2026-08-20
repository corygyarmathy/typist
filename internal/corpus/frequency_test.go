package corpus

import (
	"encoding/json"
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/corygyarmathy/typist/internal/corpus/gen"
)

// C3 - the frequency-validation test.
//
// What this file claims is narrow and worth stating plainly: the embedded
// artifact is real English, not noise. It is NOT a claim that the artifact
// matches the Norvig/Mayzner reference closely - it cannot, and should not.
// The reference counts 3.9 trillion characters of Google Books; corpus.json
// counts ~2 million characters of three works of literary narrative. The
// tolerances below are chosen to be loose enough to survive swapping a source
// book and tight enough that shuffled or synthetic letters would fail.

// Frequency data from https://norvig.com/mayzner.html, retrieved 2026-08-20
var norvigLetterPct = map[string]float64{
	"e": 12.49, "t": 9.28, "a": 8.04, "o": 7.64, "i": 7.57, "n": 7.23,
	"s": 6.51, "r": 6.28, "h": 5.05, "l": 4.07, "d": 3.82, "c": 3.34,
	"u": 2.73, "m": 2.51, "f": 2.40, "p": 2.14, "g": 1.87, "w": 1.68,
	"y": 1.66, "b": 1.48, "v": 1.05, "k": 0.54, "x": 0.23, "j": 0.16,
	"q": 0.12, "z": 0.09,
}

// norvigTopBigrams is the reference's 20 most frequent bigrams, in order,
// from the same page and retrieval.
var norvigTopBigrams = []string{
	"th", "he", "in", "er", "an", "re", "on", "at", "en", "nd",
	"ti", "es", "or", "te", "of", "ed", "is", "it", "al", "ar",
}

const (
	// letterTolerancePct is the permitted gap, in percentage points, between a
	// letter's share of the artifact and its share of the reference.
	//
	// Measured against the committed artifact the largest gap is h, at 1.65
	// points (6.70% here vs 5.05% in the reference), then c at 1.05 and i at
	// 0.81. The h skew is explainable rather than random: three works of
	// literary narrative are dense in the/that/this/he/his/her, and the bigram
	// assertion below sees the same thing from the other side - "ha" and "hi"
	// rank in this corpus's top 20 and not in the reference's.
	//
	// 2.0 therefore clears the real deviation with headroom while still
	// failing on anything that is not English. Do not tighten below ~1.8
	// without re-measuring; 1.5 fails today.
	letterTolerancePct = 2.0

	// leadingRunLen is how far down the frequency order the artifact and the
	// reference are compared, as a SET rather than a sequence.
	//
	// Six is not a round number, it is the measured boundary. Both orders open
	// with the same six letters {e,t,a,o,i,n} (this corpus ranks n above i;
	// the reference is the other way round, which is why this is a set
	// comparison). At seven they diverge for real: h places 7th here against
	// s in the reference, for the same reason the h tolerance comment gives.
	// The head of a frequency order is stable across corpora; the middle is
	// not, and asserting further down would be pinning an accident.
	leadingRunLen = 6

	// minTopBigramOverlap is how many of the reference's 20 most frequent
	// bigrams must also appear in this artifact's top 20. Measured overlap is
	// 15; 12 leaves room for a source swap to move a few without the test
	// becoming a rubber stamp.
	minTopBigramOverlap = 12
	topBigramWindow     = 20
)

// artifactCounts parses the embedded artifact. Every assertion in this file
// reads the raw counts rather than the Provider's derived fields, because the
// claim under test is about the data, not about the loading code.
func artifactCounts(t *testing.T) gen.Counts {
	t.Helper()
	var c gen.Counts
	if err := json.Unmarshal(corpusJSON, &c); err != nil {
		t.Fatalf("parsing embedded corpus.json: %v", err)
	}
	return c
}

func isLowerAZ(r rune) bool { return r >= 'a' && r <= 'z' }

// TestArtifact_WellFormed is the sanity gate: it runs before any statistics,
// so a malformed artifact fails with "j is not a letter" rather than with a
// confusing frequency mismatch twenty lines later.
func TestArtifact_WellFormed(t *testing.T) {
	c := artifactCounts(t)

	if len(c.Letters) != 26 {
		t.Errorf("len(letters) = %d, want 26 (got %v)",
			len(c.Letters), slices.Sorted(maps.Keys(c.Letters)))
	}

	for s, n := range c.Letters {
		r := []rune(s)
		if len(r) != 1 || !isLowerAZ(r[0]) {
			t.Errorf("letter key %q is not a single a-z character", s)
		}
		if n <= 0 {
			t.Errorf("letter %q has count %d, want > 0", s, n)
		}
	}

	for s, n := range c.Bigrams {
		r := []rune(s)
		if len(r) != 2 || !isLowerAZ(r[0]) || !isLowerAZ(r[1]) {
			t.Errorf("bigram key %q is not exactly two a-z characters", s)
		}
		if n <= 0 {
			t.Errorf("bigram %q has count %d, want > 0", s, n)
		}
	}

	if len(c.Bigrams) == 0 {
		t.Fatal("bigrams table is empty")
	}
}

// TestLetterFrequencies_MatchReference is the core "this is English" claim.
func TestLetterFrequencies_MatchReference(t *testing.T) {
	c := artifactCounts(t)

	var total int
	for _, n := range c.Letters {
		total += n
	}
	if total == 0 {
		t.Fatal("letter counts total zero")
	}

	for letter, wantPct := range norvigLetterPct {
		gotPct := 100 * float64(c.Letters[letter]) / float64(total)
		if diff := math.Abs(gotPct - wantPct); diff > letterTolerancePct {
			t.Errorf("letter %q: got %.2f%%, reference %.2f%% (diff %.2f, tolerance %.2f)",
				letter, gotPct, wantPct, diff, letterTolerancePct)
		}
	}
}

// TestKeyOrder_LeadingRunMatchesReference pins the head of the unlock order,
// which is the part of the corpus the engine is most sensitive to: it is
// literally the order in which a learner meets the alphabet, and
// InitialCompetency seeds from its first startingKeys entries.
func TestKeyOrder_LeadingRunMatchesReference(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	order := p.KeyOrder()
	if len(order) < leadingRunLen {
		t.Fatalf("len(KeyOrder()) = %d, want at least %d", len(order), leadingRunLen)
	}

	refOrder := slices.SortedFunc(maps.Keys(norvigLetterPct), func(a, b string) int {
		return cmpDesc(norvigLetterPct[a], norvigLetterPct[b])
	})

	got := make(map[string]bool, leadingRunLen)
	for _, r := range order[:leadingRunLen] {
		got[string(r)] = true
	}

	for _, letter := range refOrder[:leadingRunLen] {
		if !got[letter] {
			t.Errorf("leading run of KeyOrder() is missing %q; got %v, reference %v",
				letter, string(order[:leadingRunLen]), refOrder[:leadingRunLen])
		}
	}
}

// TestTopBigrams_OverlapReference checks the pair distribution, which is the
// half of the artifact that letter frequencies cannot vouch for: a corpus of
// correctly-distributed but randomly-ordered letters would pass the letter
// test above and fail this one.
func TestTopBigrams_OverlapReference(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	ngrams := p.NgramsByFrequency()
	if len(ngrams) < topBigramWindow {
		t.Fatalf("len(NgramsByFrequency()) = %d, want at least %d", len(ngrams), topBigramWindow)
	}

	top := make(map[string]bool, topBigramWindow)
	for _, g := range ngrams[:topBigramWindow] {
		top[g] = true
	}

	var overlap int
	var missing []string
	for _, g := range norvigTopBigrams {
		if top[g] {
			overlap++
		} else {
			missing = append(missing, g)
		}
	}

	if overlap < minTopBigramOverlap {
		t.Errorf("top-%d bigram overlap = %d, want at least %d (missing %v; got %v)",
			topBigramWindow, overlap, minTopBigramOverlap, missing, ngrams[:topBigramWindow])
	}
}

// TestTransitions_NonEmptyForEveryLetter guards the lesson generator: a seed
// character with no outgoing candidates starves the walk immediately and ends
// the word after one character. Every letter currently has at least one
// successor - q has exactly one (u), which is correct English, not a defect.
func TestTransitions_NonEmptyForEveryLetter(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	for r := 'a'; r <= 'z'; r++ {
		cands := p.Transitions(string(r))
		if len(cands) == 0 {
			t.Errorf("Transitions(%q) is empty: a word seeded on %q would starve", r, r)
		}
	}
}

func cmpDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}
