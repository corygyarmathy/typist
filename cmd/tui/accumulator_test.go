package main

import (
	"maps"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/openapi"
)

var testNow = time.Date(
	2026, 10, 1,
	0, 0, 0, 0,
	time.UTC,
)

func TestAccumulator(t *testing.T) {

	type press struct {
		r     rune
		delay time.Duration
	}

	// at 60 wpm, w/ 5 chars per word, each key press is 200 ms
	pressTime := 200 * time.Millisecond

	presses := []press{
		{r: 'e', delay: pressTime},
		{r: 'z', delay: pressTime}, // first-try error
		{r: 'a', delay: pressTime},
		{r: 't', delay: pressTime},
		{r: ' ', delay: pressTime},
		{r: 't', delay: pressTime},
		{r: 'o', delay: pressTime},
	}

	lessonWords := []string{"eat", "to"}
	a := newAccumulator(lessonWords, testNow)
	now := testNow
	for _, p := range presses {
		now = now.Add(p.delay)
		a.Press(p.r, now)
	}

	if !a.Done() {
		t.Fatalf("not done after %d presses", len(presses))
	}

	sub := a.Submission()

	expKeys := make(map[string]openapi.Observation)
	expKeys["e"] = openapi.Observation{Attempts: 1, Errors: 0, TotalMillis: 200}
	expKeys["a"] = openapi.Observation{Attempts: 1, Errors: 1, TotalMillis: 400}
	expKeys["t"] = openapi.Observation{Attempts: 2, Errors: 0, TotalMillis: 400}
	expKeys["o"] = openapi.Observation{Attempts: 1, Errors: 0, TotalMillis: 200}

	if !maps.Equal(sub.Keys, expKeys) {
		t.Errorf("submitted keys != expected keys")
		t.Logf("expected keys: %v", expKeys)
		t.Logf("submitted keys: %v", sub.Keys)
	}

	expNgrams := make(map[string]openapi.Observation)
	expNgrams["ea"] = openapi.Observation{Attempts: 1, Errors: 1, TotalMillis: 600}
	expNgrams["at"] = openapi.Observation{Attempts: 1, Errors: 1, TotalMillis: 600}
	expNgrams["to"] = openapi.Observation{Attempts: 1, Errors: 0, TotalMillis: 400}

	if !maps.Equal(sub.Ngrams, expNgrams) {
		t.Errorf("submitted ngrams != expected ngrams")
		t.Logf("expected ngrams: %v", expNgrams)
		t.Logf("submitted ngrams: %v", sub.Ngrams)
	}
}

// Press's bool is what the display layer reads to show a rejection, so the
// contract is asserted directly: it reports correctness, and a wrong key
// leaves the cursor where it was for the typist to try again.
func TestPressReportsWhetherTheKeyWasCorrect(t *testing.T) {
	a := newAccumulator([]string{"eat"}, testNow)

	if a.Press('z', testNow) {
		t.Error("Press('z') = true at a position expecting 'e', want false")
	}
	if a.cursor != 0 {
		t.Errorf("cursor = %d after a wrong key, want 0 - a rejected press must not advance", a.cursor)
	}

	if !a.Press('e', testNow) {
		t.Error("Press('e') = false at a position expecting 'e', want true")
	}
	if a.cursor != 1 {
		t.Errorf("cursor = %d after a correct key, want 1", a.cursor)
	}

	// Past the end of the text there is no correct key, so every press is
	// refused - the same answer the display layer would show as a rejection.
	for !a.Done() {
		a.Press(a.text[a.cursor], testNow)
	}
	if a.Press('t', testNow) {
		t.Error("Press('t') = true after the text is complete, want false")
	}
}
