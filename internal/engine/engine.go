// Package engine contains the adaptive lesson-selection and difficulty engine.
//
// This is the heart of the application's interesting domain logic. It takes
// a snapshot of a user's competency (per-key and per-ngram scores) plus the
// available corpus, and returns the next lesson tailored to push that user
// toward their next threshold.
//
// Design goals:
//
//  1. PURE FUNCTIONS. The engine touches no I/O. It accepts state, returns
//     state. This makes it trivially testable and means we can reason about
//     its behaviour in isolation. All randomness flows through an injected
//     rand.Source so tests are deterministic.
//
//  2. NO HTTP, NO DATABASE, NO LOGGING. The service layer (progress, session)
//     calls into this package; the engine never calls out.
//
//  3. DECOUPLED FROM PERSISTENCE. The engine works on engine-local types
//     (CompetencyState, ItemScore, Observation, Result, Lesson - see types.go).
//     The progress service is responsible for translating between persisted
//     models and these types.
//
// The two key responsibilities are:
//
//   - Selection: given competency state, choose which keys and ngrams the
//     next lesson should target. Weight toward weak spots; introduce new
//     content as the user crosses thresholds.
//
//   - Scoring: given a completed lesson result, compute the score delta
//     for each key and ngram involved. Accuracy-weighted, with diminishing
//     returns on items already well-mastered.
package engine

import (
	"maps"
	"math/rand/v2"
	"time"
)

// The 'magic numbers' tuning the engine behaviour. Shared by every behaviour
// file in the package; the design doc's table (docs/engine.md,
// "Tunable constants") lists the same set with the same defaults.
//
// Implemented as const block rather than a more complex Params struct for
// simplicity. May revisit if advantageous to testing the engine, once at that
// stage of implementation.
//
//nolint:unused // TODO: remove. waiting on phase-3 to be completed.
const (
	wAccuracy             = 0.7                // weight of accuracy in instant score
	wSpeed                = 0.3                // weight of speed in instant score
	alpha                 = 0.3                // EMA smoothing; higher = more reactive
	decayTau              = 7 * 24 * time.Hour // recency decay time constant: the e-folding time in exp(-age/tau)
	unlockKeyThreshold    = 0.85               // min decayed score on all keys to unlock next
	unlockNgramThreshold  = 0.80               // min decayed score on active ngrams to advance
	minSamples            = 50                 // confidence gate before any unlock
	phaseThreshold        = 0.75               // mean key score to enter ngram-focus phase
	startingKeys          = 4                  // size of the initial unlocked set
	startingTargetWPM     = 40                 // initial target speed; held until the alphabet unlocks
	targetRaiseScore      = 0.85               // mean key decayed score needed to raise the target
	targetWPMStep         = 5                  // WPM added per target raise
	lambdaKey             = 3.0                // weak-key boost in generation
	lambdaNgramKeyPhase   = 0.5                // weak-ngram boost while the lesson is key-focused
	lambdaNgramNgramPhase = 3.0                // ngram boost once phaseIsNgrams
	lessonTargets         = 5                  // high-need items a lesson records for telemetry
	lessonWords           = 15                 // words per generated lesson
	charsPerWord          = 5                  // assumed chars per average word, used to calc. WPM
	minWordLen            = 2                  // min chars per word, used to generate words for lessons
	maxWordLen            = 6                  // max chars per word, used to generate words for lessons
)

// The engine's two entry points are package-level pure functions (not methods
// on a struct): all randomness and the current time are injected so tests are
// deterministic. See docs/engine.md for the full spec.

func NextLesson(s CompetencyState, c Corpus, now time.Time, r *rand.Rand) Lesson {
	sc := newLessonScope(s, c, now)
	seeds := sc.seeds()
	if len(seeds) == 0 {
		return Lesson{}
	}

	var lesson Lesson

	for range lessonWords {
		word := sc.word(seeds, r)
		lesson.Words = append(lesson.Words, word)
	}

	lesson.Targets = sc.targets()

	return lesson
}

func ApplyResult(s CompetencyState, c Corpus, res Result, now time.Time) CompetencyState {
	// NOTE: the progression predicates read next, not s. Otherwise the results
	// will be unexpectedly one lesson behind.

	// clone current state into local mutable object
	next := s                      // copies NgramTier, TargetWPM by value
	next.Keys = maps.Clone(s.Keys) // shallow == deep, ItemScore has no reference fields
	next.Ngrams = maps.Clone(s.Ngrams)
	if next.Keys == nil {
		next.Keys = make(map[rune]ItemScore)
	}
	if next.Ngrams == nil {
		next.Ngrams = make(map[string]ItemScore)
	}

	// update item scores
	for k, o := range res.Keys {
		// check if key present, prevent client from overriding state
		if _, ok := next.Keys[k]; !ok {
			continue
		}

		updated, ok := updateScore(s.Keys[k], o, s.TargetWPM, now)
		if !ok {
			// skipping, don't want to discard whole result b/c of one bad data point
			continue
		}
		next.Keys[k] = updated
	}

	_, inScope := activeNgramSet(next, c)

	for ngram, o := range res.Ngrams {
		if _, ok := inScope[ngram]; !ok {
			continue // out of scope; ignore whatever the client sent
		}

		updated, ok := updateScore(s.Ngrams[ngram], o, s.TargetWPM, now)
		if !ok {
			// skipping, don't want to discard whole result b/c of one bad data point
			continue
		}
		next.Ngrams[ngram] = updated
	}

	// perform unlocks / advances based on updated scores
	if k, ok := nextKeyToUnlock(next, c, now); ok {
		next.Keys[k] = ItemScore{}
	}

	if shouldAdvanceNgramTier(next, c, now) {
		// clamp to prevent run-away value
		next.NgramTier = min(len(c.NgramsByFrequency()), next.NgramTier+1)
	}

	if shouldRaiseTarget(next, c, now) {
		next.TargetWPM += targetWPMStep
	}

	return next
}
