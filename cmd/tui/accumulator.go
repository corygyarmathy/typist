package main

import (
	"strings"
	"time"

	"github.com/corygyarmathy/typist/internal/openapi"
)

type accumulator struct {
	text        []rune
	positions   []position
	cursor      int
	lastCorrect time.Time
}

type position struct {
	firstTryError bool
	millis        int64 // interval from the correct keystroke at i-1
}

func newAccumulator(words []string, now time.Time) *accumulator {
	text := []rune(strings.Join(words, " "))
	return &accumulator{
		text:        text,
		lastCorrect: now, // started from when lesson exists
		positions:   make([]position, len(text)),
	}
}

func (a *accumulator) Press(r rune, now time.Time) {
	if a.Done() {
		return
	}

	// incorrect press
	if r != a.text[a.cursor] {
		a.positions[a.cursor].firstTryError = true
		return
	}

	// correct press
	diff := now.Sub(a.lastCorrect)
	a.positions[a.cursor].millis = diff.Milliseconds()

	a.lastCorrect = now
	a.cursor++
}

func (a *accumulator) Done() bool {
	return a.cursor >= len(a.text)
}

func (a *accumulator) Submission() openapi.SessionSubmission {
	submission := openapi.SessionSubmission{
		Keys:   make(map[string]openapi.Observation),
		Ngrams: map[string]openapi.Observation{},
	}

	// keys
	for i, r := range a.text {
		// skip spaces
		if r == ' ' {
			continue
		}

		key := string(r)
		obs := submission.Keys[key] // absent -> the zero Observation
		obs.Attempts++
		if a.positions[i].firstTryError {
			obs.Errors++
		}
		obs.TotalMillis += int(a.positions[i].millis)
		submission.Keys[key] = obs
	}

	// ngrams
	for i := 0; i+1 < len(a.text); i++ {
		// skip spaces
		if a.text[i] == ' ' || a.text[i+1] == ' ' {
			continue
		}

		key := string(a.text[i : i+2])
		obs := submission.Ngrams[key] // absent -> the zero Observation
		obs.Attempts++
		if a.positions[i].firstTryError || a.positions[i+1].firstTryError {
			obs.Errors++
		}
		obs.TotalMillis += int(a.positions[i].millis) + int(a.positions[i+1].millis)
		submission.Ngrams[key] = obs
	}

	return submission
}

// Errors counts positions whose first keystroke was wrong. Display only - the
// submission derives its own per-item counts from the same records.
func (a *accumulator) Errors() int {
	n := 0
	for _, p := range a.positions {
		if p.firstTryError {
			n++
		}
	}
	return n
}
