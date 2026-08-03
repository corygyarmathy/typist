package engine

import "time"

var testNow = time.Date(
	2026, 10, 1,
	0, 0, 0, 0,
	time.UTC,
)

// Compile-time assertion that the fake satisfies the interface
var _ Corpus = fakeCorpus{}

type fakeCorpus struct {
	ngramsByFrequency []string // frequency-ordered ngrams
	keyOrder          []rune   // frequency-ordered keys
	transitions       map[string][]Candidate
}

func (f fakeCorpus) StartingKeys() int {
	return 0 // ignored in this test
}

func (f fakeCorpus) KeyOrder() []rune { // frequency order for unlocking
	return f.keyOrder
}

func (f fakeCorpus) NgramsByFrequency() []string { // frequency-ranked; defines tiers
	return f.ngramsByFrequency
}

func (f fakeCorpus) Transitions(context string) []Candidate { // for the generator
	return f.transitions[context]
}

func keysFrom(s string) map[rune]ItemScore {
	keys := make(map[rune]ItemScore)
	for _, r := range s {
		keys[r] = ItemScore{}
	}
	return keys
}

func keysScoreFrom(s string, score float64, samples int, lastPracticed time.Time) map[rune]ItemScore {
	keys := make(map[rune]ItemScore)
	for _, r := range s {
		keys[r] = ItemScore{Score: score, Samples: samples, LastPracticed: lastPracticed}
	}
	return keys
}

func ngramsScoreFrom(n []string, score float64, samples int, lastPracticed time.Time) map[string]ItemScore {
	ngrams := make(map[string]ItemScore)
	for _, s := range n {
		ngrams[s] = ItemScore{Score: score, Samples: samples, LastPracticed: lastPracticed}
	}
	return ngrams
}

func keysObservationFrom(k map[rune]ItemScore, attempts int, errors int, totalMillis float64) map[rune]Observation {
	keys := make(map[rune]Observation)
	for r := range k {
		keys[r] = Observation{Attempts: attempts, Errors: errors, TotalMillis: totalMillis}
	}
	return keys
}

func ngramsObservationFrom(n map[string]ItemScore, attempts int, errors int, totalMillis float64) map[string]Observation {
	ngrams := make(map[string]Observation)
	for s := range n {
		ngrams[s] = Observation{Attempts: attempts, Errors: errors, TotalMillis: totalMillis}
	}
	return ngrams
}
