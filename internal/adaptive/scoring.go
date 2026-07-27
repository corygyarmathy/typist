package adaptive

import (
	"math"
	"time"
)

// Scoring is a separate file because the math is interesting enough to
// deserve its own test file (scoring_test.go) and grows independently of
// lesson selection.

// instant score for one item's observation this lesson: accuracy-weighted, [0,1].
// Returns !ok if impossible input (o.Attempts, o.TotalMillis, targetWPM == 0).
func instant(o Observation, targetWPM int) (score float64, ok bool) {
	if o.Attempts == 0 || o.TotalMillis == 0 || targetWPM == 0 { // invalid inputs
		return 0, false
	}

	msPerMin := int(time.Minute / time.Millisecond)

	accuracy := float64(o.Attempts-o.Errors) / float64(o.Attempts)  // [0,1]
	meanItemMs := o.TotalMillis / float64(o.Attempts)               // avg. time per item
	targetMs := float64(msPerMin) / float64(targetWPM*charsPerWord) // ms per item at target
	speed := min(1, max(0, targetMs/meanItemMs))                    // 1.0 == at/above target
	score = wAccuracy*accuracy + wSpeed*speed

	return score, true
}

// updateScore folds an observation into an item's stored score: EMA of
// instant, bump Samples, set LastPracticed. New item (Samples=0) ->
// Score == instant. Returns prev, !ok if impossible input (o.Attempts,
// o.TotalMillis, targetWPM == 0).
func updateScore(prev ItemScore, o Observation, targetWPM int, now time.Time) (itemScore ItemScore, ok bool) {
	instScore, ok := instant(o, targetWPM)
	if !ok {
		return prev, false
	}
	var score float64
	if prev.Samples == 0 {
		score = instScore
	} else {
		score = alpha*instScore + (1-alpha)*prev.Score // EMA of instant score
	}

	return ItemScore{
		Score:         score,
		Samples:       prev.Samples + o.Attempts,
		LastPracticed: now,
	}, true
}

// decayedScore gets effective score at read time: raw Score decayed by age
// since LastPracticed.
func decayedScore(s ItemScore, now time.Time) float64 {
	age := now.Sub(s.LastPracticed)
	age = max(0, age) // Clamp to 0, accounting for LastPracticed being in the future

	// Use tau in the same unit as age.
	ageSeconds := age.Seconds()
	tauSeconds := decayTau.Seconds()

	exponent := -ageSeconds / tauSeconds
	return s.Score * math.Exp(exponent)
}
