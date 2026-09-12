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
