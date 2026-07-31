package engine

import (
	"slices"
	"testing"
	"time"
)

// Compile-time assertion that the fake satisfies the interface
var _ Corpus = fakeCorpus{}

type fakeCorpus struct {
	ngramsByFrequency []string // frequency-ordered ngrams
	keyOrder          []rune   // frequency-ordered keys
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
	return nil // ignored in this test
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

func TestKeysUnlocked(t *testing.T) {
	tests := []struct {
		name         string
		ngram        string
		keys         map[rune]ItemScore
		wantUnlocked bool
	}{
		{
			"every rune present",
			"th",
			keysFrom("the"),
			true,
		},
		{
			"some runes missing",
			"th",
			keysFrom("te"),
			false},
		{
			"all runes missing",
			"th",
			keysFrom("ab"),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unlocked := keysUnlocked(tt.ngram, tt.keys)
			if unlocked != tt.wantUnlocked {
				t.Errorf("unlocked = %v, want %v", unlocked, tt.wantUnlocked)
			}
		})
	}
}

func TestActiveNgrams(t *testing.T) {
	tests := []struct {
		name       string
		s          CompetencyState
		c          fakeCorpus
		wantActive []string
	}{
		{
			name: "ngram tier over the ngram list length",
			s: CompetencyState{
				Keys:      keysFrom("theour"),
				NgramTier: 10,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantActive: []string{"th", "or", "eo", "ur", "he", "ou"},
		},
		{
			name: "ngram tier partial cut of ngrams",
			s: CompetencyState{
				Keys:      keysFrom("theour"),
				NgramTier: 3,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantActive: []string{"th", "or", "eo"},
		},
		{
			name: "ngram tier negative",
			s: CompetencyState{
				Keys:      keysFrom("theour"),
				NgramTier: -10,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantActive: []string{},
		},
		{
			name: "ngram tier zero",
			s: CompetencyState{
				Keys:      keysFrom("theour"),
				NgramTier: 0,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantActive: []string{},
		},
		{
			name: "locked-rune ngram exclusion",
			s: CompetencyState{
				Keys:      keysFrom("theor"), // missing 'u'
				NgramTier: 10,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantActive: []string{"th", "or", "eo", "he"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active := activeNgrams(tt.s, tt.c)

			if !slices.Equal(active, tt.wantActive) {
				t.Errorf(
					"did not receive wanted ngrams.\nWanted: '%v'.\nGot:    '%v'",
					tt.wantActive, active,
				)
			}
		})
	}
}

func TestNextKeyToUnlock(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		s       CompetencyState
		c       fakeCorpus
		wantKey rune
		wantOk  bool
	}{
		{
			name: "all keys over unlock threshold",
			s: CompetencyState{
				Keys: keysScoreFrom(
					"theour",
					unlockKeyThreshold+0.1,
					minSamples+1,
					now,
				),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r', 'z', 'q', 'x'},
			},
			wantKey: 'z',
			wantOk:  true,
		},
		{
			name: "empty s.Keys unlocks next key",
			s:    CompetencyState{},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r', 'z'},
			},
			wantKey: 't',
			wantOk:  true,
		},
		{
			name: "key under unlock sample threshold",
			s: CompetencyState{
				Keys: keysScoreFrom(
					"t",
					unlockKeyThreshold+0.1,
					minSamples-1,
					now,
				),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'z'},
			},
			wantKey: 0,
			wantOk:  false,
		},
		{
			name: "key under unlock score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom(
					"t",
					unlockKeyThreshold-0.1,
					minSamples+1,
					now,
				),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'z'},
			},
			wantKey: 0,
			wantOk:  false,
		},
		{
			name: "key under unlock decayed score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom(
					"t",
					1,
					minSamples+1,
					now.AddDate(0, 0, -30), //y,m,d
				),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'z'},
			},
			wantKey: 0,
			wantOk:  false,
		},
		{
			name: "only unlock key if more can be unlocked",
			s: CompetencyState{
				Keys: keysScoreFrom(
					"tz",
					unlockKeyThreshold+0.1,
					minSamples+1,
					now,
				),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'z'},
			},
			wantKey: 0,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := nextKeyToUnlock(tt.s, tt.c, now)
			if key != tt.wantKey {
				t.Errorf("want key '%c', got '%c'", tt.wantKey, key)
			}
			if ok != tt.wantOk {
				t.Errorf("want ok '%v', got '%v'", tt.wantOk, ok)
			}
		})
	}
}

func TestShouldAdvanceNgramTier(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		s           CompetencyState
		c           fakeCorpus
		wantAdvance bool
	}{
		{
			name: "all active ngrams over unlock threshold",
			s: CompetencyState{
				Keys: keysFrom("theour"),
				Ngrams: ngramsScoreFrom(
					[]string{"th", "or", "eo", "ur", "he", "ou"},
					unlockNgramThreshold+0.1,
					minSamples+1,
					now,
				),
				NgramTier: 5,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantAdvance: true,
		},
		{
			name: "active ngram never practiced",
			s: CompetencyState{
				Keys: keysFrom("theour"),
				Ngrams: ngramsScoreFrom(
					[]string{"th", "or"},
					unlockNgramThreshold+0.1,
					minSamples+1,
					now,
				),
				NgramTier: 3,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantAdvance: false,
		},
		{
			name: "ngram below unlock score threshold",
			s: CompetencyState{
				Keys: keysFrom("theour"),
				Ngrams: ngramsScoreFrom(
					[]string{"th", "or", "eo", "ur", "he", "ou"},
					unlockNgramThreshold-0.1,
					minSamples+1,
					now,
				),
				NgramTier: 6,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantAdvance: false,
		},
		{
			name: "ngram below unlock samples threshold",
			s: CompetencyState{
				Keys: keysFrom("theour"),
				Ngrams: ngramsScoreFrom(
					[]string{"th", "or", "eo", "ur", "he", "ou"},
					unlockNgramThreshold+0.1,
					minSamples-1,
					now,
				),
				NgramTier: 6,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantAdvance: false,
		},
		{
			name: "ngram below decayed score threshold",
			s: CompetencyState{
				Keys: keysFrom("theour"),
				Ngrams: ngramsScoreFrom(
					[]string{"th", "or", "eo", "ur", "he", "ou"},
					unlockNgramThreshold+0.1,
					minSamples+1,
					now.AddDate(0, 0, -30), //y,m,d
				),
				NgramTier: 6,
			},
			c: fakeCorpus{
				ngramsByFrequency: []string{"th", "or", "eo", "ur", "he", "ou"},
			},
			wantAdvance: false,
		},
		{
			name:        "empty ngrams",
			s:           CompetencyState{},
			c:           fakeCorpus{},
			wantAdvance: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advance := shouldAdvanceNgramTier(tt.s, tt.c, now)
			if advance != tt.wantAdvance {
				t.Errorf("want ngram advance '%v', got '%v'", tt.wantAdvance, advance)
			}
		})
	}
}

func TestShouldRaiseTarget(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		s         CompetencyState
		c         fakeCorpus
		wantRaise bool
	}{
		{
			name: "all keys unlocked, over mean score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					targetRaiseScore,
					0,
					now),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r'},
			},
			wantRaise: true,
		},
		{
			name: "not all keys unlocked, over mean score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					targetRaiseScore,
					0,
					now),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r', 'z', 'q', 'x'},
			},
			wantRaise: false,
		},
		{
			name: "all keys unlocked, below mean score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					targetRaiseScore-0.1,
					0,
					now),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r'},
			},
			wantRaise: false,
		},
		{
			name: "all keys unlocked, below mean decayed score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					targetRaiseScore+0.1,
					0,
					now.AddDate(0, 0, -30))}, // y,m,d
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r'},
			},
			wantRaise: false,
		},
		{
			name:      "empty keys",
			s:         CompetencyState{},
			c:         fakeCorpus{},
			wantRaise: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raise := shouldRaiseTarget(tt.s, tt.c, now)
			if raise != tt.wantRaise {
				t.Errorf("want raise WPM target '%v', got '%v'", tt.wantRaise, raise)
			}
		})
	}
}

func TestPhaseIsNgrams(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		s         CompetencyState
		c         fakeCorpus
		wantPhase bool
	}{
		{
			name: "all keys unlocked, over mean score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					phaseThreshold,
					10,
					now),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r'},
			},
			wantPhase: true,
		},
		{
			name: "not all keys unlocked, over mean score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					phaseThreshold,
					10,
					now),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r', 'z', 'q', 'x'},
			},
			wantPhase: false,
		},
		{
			name: "all keys unlocked, below mean score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					phaseThreshold-0.1,
					10,
					now),
			},
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r'},
			},
			wantPhase: false,
		},
		{
			name: "all keys unlocked, below mean decayed score threshold",
			s: CompetencyState{
				Keys: keysScoreFrom("theour",
					phaseThreshold+0.1,
					10,
					now.AddDate(0, 0, -30))}, // y,m,d
			c: fakeCorpus{
				keyOrder: []rune{'t', 'h', 'e', 'o', 'u', 'r'},
			},
			wantPhase: false,
		},
		{
			name:      "empty keys",
			s:         CompetencyState{},
			c:         fakeCorpus{},
			wantPhase: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase := phaseIsNgrams(tt.s, tt.c, now)
			if phase != tt.wantPhase {
				t.Errorf("want ngram phase '%v', got '%v'", tt.wantPhase, phase)
			}
		})
	}
}
