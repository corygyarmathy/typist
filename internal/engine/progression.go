package engine

import (
	"iter"
	"maps"
	"time"
)

// activeNgrams returns all ngrams that are within the tier and have their
// constituent keys unlocked.
func activeNgrams(s CompetencyState, c Corpus) []string {
	ngrams := c.NgramsByFrequency()
	tier := max(0, min(len(ngrams), s.NgramTier))
	ngrams = ngrams[:tier]

	var active []string
	for _, ngram := range ngrams {
		if keysUnlocked(ngram, s.Keys) {
			active = append(active, ngram)
		}
	}

	return active
}

func activeNgramSet(s CompetencyState, c Corpus) (active []string, inScope map[string]struct{}) {
	// Ngram activeness is derived, so compute it once.
	active = activeNgrams(s, c)
	inScope = make(map[string]struct{}, len(active))
	for _, n := range active {
		inScope[n] = struct{}{}
	}

	return active, inScope
}

// keysUnlocked reports whether every rune of the given string is an unlocked
// key.
func keysUnlocked(s string, keys map[rune]ItemScore) bool {
	for _, r := range s {
		if _, ok := keys[r]; !ok {
			return false
		}
	}
	return true
}

// nextKeyToUnlock returns the next key to unlock if there are further keys to
// unlock, and if all keys meet the unlock threshold. Also returns ok status
// for whether key unlocked or not.
func nextKeyToUnlock(s CompetencyState, c Corpus, now time.Time) (key rune, ok bool) {
	keys := c.KeyOrder()
	var unlockKey rune

	// not found means its the next key to unlock
	found := false
	for _, k := range keys {
		if _, ok = s.Keys[k]; !ok {
			found = true
			unlockKey = k
			break
		}
	}
	if !found {
		return 0, false
	}

	if !allMastered(
		maps.Values(s.Keys),
		unlockKeyThreshold,
		now,
	) {
		return 0, false
	}

	return unlockKey, true
}

// shouldAdvanceNgramTier reports whether the ngram tier should advance,
// depending on whether all active ngrams meet the score and sample thresholds.
// Returns false if no active ngrams unlocked.
func shouldAdvanceNgramTier(s CompetencyState, c Corpus, now time.Time) bool {
	active := activeNgrams(s, c)

	if len(active) == 0 {
		return false
	}

	return allMastered(
		scoresOf(active, s.Ngrams),
		unlockNgramThreshold,
		now,
	)
}

// shouldRaiseTarget reports whether the WPM target should increase, depending
// on whether all keys are unlocked and are over the score threshold.
func shouldRaiseTarget(s CompetencyState, c Corpus, now time.Time) bool {
	// all keys not unlocked
	if !keysUnlocked(string(c.KeyOrder()), s.Keys) {
		return false
	}

	return meanKeyDecayedScore(s, now) >= targetRaiseScore
}

// phaseIsNgrams reports whether the lesson should be softly key or ngram
// focused. Starts key focused. After all keys unlocked and mean decayedScore
// above phaseThreshold, lesson become ngram focussed.
func phaseIsNgrams(s CompetencyState, c Corpus, now time.Time) bool {
	// all keys not unlocked
	if !keysUnlocked(string(c.KeyOrder()), s.Keys) {
		return false
	}

	return meanKeyDecayedScore(s, now) >= phaseThreshold
}

// allMastered reports whether every item clears both the score threshold and
// the sample floor. Returns true if items is empty.
func allMastered(items iter.Seq[ItemScore], threshold float64, now time.Time) bool {
	for item := range items {
		if decayedScore(item, now) < threshold || item.Samples < minSamples {
			return false
		}
	}
	return true
}

// meanKeyDecayedScore returns the mean key decayed score.
func meanKeyDecayedScore(s CompetencyState, now time.Time) float64 {
	if len(s.Keys) == 0 {
		return 0
	}

	var sum float64
	for _, v := range s.Keys {
		sum += decayedScore(v, now)
	}
	mean := sum / float64(len(s.Keys))
	return mean
}

// scoresOf constructs an iterator from the active ngrams and the scored ngrams
// in CompetencyState. A name with no entry in the map yields the zero
// ItemScore.
func scoresOf(names []string, m map[string]ItemScore) iter.Seq[ItemScore] {
	return func(yield func(ItemScore) bool) {
		for _, n := range names {
			if !yield(m[n]) {
				return
			}
		}
	}
}
