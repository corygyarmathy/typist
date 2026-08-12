package engine

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// weightedCandidate pairs a possible next character with its sampling weight.
// Only the character survives sampling, so this deliberately does not carry the
// Candidate it came from: a seed has no corpus frequency at all, and a mid-word
// candidate's frequency is already folded into weight.
type weightedCandidate struct {
	char   rune
	weight float64
}

// lessonScope is everything that stays constant for the duration of one lesson:
// derived once in NextLesson, read at every step of every word. It is not the
// injected Params struct of plan Decision 1 - it holds no tunables, only values
// derived from (state, corpus, now), and the entry points stay package-level
// pure functions.
type lessonScope struct {
	state   CompetencyState
	corpus  Corpus
	active  []string            // active ngrams in frequency order; targets() reads this
	inScope map[string]struct{} // the same set, for the O(1) gate in candidates()
	lambdaN float64             // phase-scaled ngram boost
	now     time.Time
}

func newLessonScope(s CompetencyState, c Corpus, now time.Time) lessonScope {
	active, inScope := activeNgramSet(s, c)

	lambdaN := lambdaNgramKeyPhase
	if phaseIsNgrams(s, c, now) {
		lambdaN = lambdaNgramNgramPhase
	}

	return lessonScope{
		state:   s,
		corpus:  c,
		active:  active,
		inScope: inScope,
		lambdaN: lambdaN,
		now:     now,
	}
}

// seeds is the unlocked key set as sampling candidates - how each word starts.
//
// There is no ngram factor here, and it is not an omission: a word start has no
// preceding character, so there is no ngram to score. Passing the key through
// need as a 1-rune string would hit the Keys map again and double-count it.
// Weights are at least 1.0, so seeds can never total zero and starve the walk.
func (sc lessonScope) seeds() []weightedCandidate {
	cands := make([]weightedCandidate, 0)

	keys := sc.corpus.KeyOrder()
	for _, k := range keys {
		if _, ok := sc.state.Keys[k]; ok {
			weight := 1 + lambdaKey*need(string(k), sc.state, sc.now)
			wCan := weightedCandidate{char: k, weight: weight}
			cands = append(cands, wCan)
		}
	}

	return cands
}

// corpus candidates filtered to unlocked keys, weighted
func (sc lessonScope) candidates(context string) []weightedCandidate {
	cands := sc.corpus.Transitions(context)
	unlocked := make([]weightedCandidate, 0)

	for _, c := range cands {
		if _, ok := sc.state.Keys[c.Char]; ok {
			// calc. weight
			g := context + string(c.Char)
			keyFactor := 1 + lambdaKey*need(string(c.Char), sc.state, sc.now)
			var ngramFactor float64
			if _, ok := sc.inScope[g]; ok {
				ngramFactor = 1 + sc.lambdaN*need(g, sc.state, sc.now)
			} else {
				ngramFactor = 1.0
			}
			weight := math.Pow(c.Freq, freqExponent) * keyFactor * ngramFactor

			// append unlocked, weighted candidate
			w := weightedCandidate{char: c.Char, weight: weight}
			unlocked = append(unlocked, w)
		}
	}

	return unlocked
}

// one word: seed a first char, then walk until the target length or starvation
func (sc lessonScope) word(seeds []weightedCandidate, r *rand.Rand) string {
	targetLen := r.IntN(maxWordLen-minWordLen+1) + minWordLen
	context, ok := weightedSample(seeds, r)
	if !ok {
		return ""
	}

	var b strings.Builder

	b.Grow(targetLen)
	b.WriteRune(context)

	for range targetLen - 1 {
		cands := sc.candidates(string(context))
		sample, ok := weightedSample(cands, r)
		if !ok { // starvation, finish word
			break
		}

		b.WriteRune(sample)
		context = sample
	}

	return b.String()
}

// targets records the highest-need active items: what this lesson was built to
// exercise (telemetry only - it does not steer the walk). Keys and ngrams
// compete in a single ranking, because once the phase flips to ngrams a weak
// ngram is more worth reporting than the 5th-weakest key.
func (sc lessonScope) targets() []string {
	// Collect in corpus order - keys first, then ngrams. This is never the
	// output order; it is only what breaks ties, which is why the sort below
	// must be stable. Both sources are slices, so map iteration order never
	// reaches the output.
	names := make([]string, 0, len(sc.state.Keys)+len(sc.active))
	for _, k := range sc.corpus.KeyOrder() {
		if _, ok := sc.state.Keys[k]; ok {
			names = append(names, string(k))
		}
	}
	names = append(names, sc.active...)

	// Descending by need: b before a.
	slices.SortStableFunc(names, func(a, b string) int {
		return cmp.Compare(
			need(b, sc.state, sc.now),
			need(a, sc.state, sc.now))
	})

	return names[:min(lessonTargets, len(names))]
}

// need returns how needed a given item is from 1-0.
func need(item string, s CompetencyState, now time.Time) float64 {
	// 1 rune == key
	var itemScore ItemScore
	if utf8.RuneCountInString(item) == 1 {
		key, _ := utf8.DecodeRuneInString(item)
		itemScore = s.Keys[key]
	} else { // ngram
		itemScore = s.Ngrams[item]
	}

	return 1 - decayedScore(itemScore, now)
}

// weightedSample picks one candidate at random, where some candidates are more
// likely than others in proportion to their weight.
func weightedSample(cands []weightedCandidate, r *rand.Rand) (sample rune, ok bool) {
	if len(cands) == 0 {
		return 0, false
	}

	var total float64 // total range
	for _, c := range cands {
		total += c.weight
	}

	if total <= 0.0 {
		return 0, false
	}

	// where randomly selected within total range
	draw := r.Float64() * total

	return pick(cands, draw)
}

// pick walks the cumulative weights and returns the candidate character whose
// interval contains draw.
func pick(cands []weightedCandidate, draw float64) (sample rune, ok bool) {
	if len(cands) == 0 {
		return 0, false
	}

	var sum float64
	for _, c := range cands {
		sum += c.weight
		if sum > draw {
			return c.char, true
		}
	}

	// Floating-point accumulation can leave the final cumulative a hair below
	// the caller-computed total, so this is reachable.
	lastIndex := len(cands) - 1
	return cands[lastIndex].char, true
}
