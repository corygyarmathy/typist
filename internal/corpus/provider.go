package corpus

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/corygyarmathy/typist/internal/corpus/gen"
	"github.com/corygyarmathy/typist/internal/engine"
)

// Provider serves the embedded corpus. It satisfies the Corpus interface that
// internal/engine defines and consumes: the consumer owns the interface,
// this package owns the data.
//
// Note the import direction that implies, which is deliberate and the reverse
// of what "corpus is lower-level than the engine" would suggest: implementing
// these methods makes this package import internal/engine, because the
// signatures speak the engine's vocabulary (engine.Candidate). The
// implementer depends on the consumer's abstraction, so internal/engine can
// be built and tested with this package absent entirely. The reasoning is
// recorded on engine.Candidate.
//
//go:embed data/corpus.json
var corpusJSON []byte

var _ engine.Corpus = (*Provider)(nil)

type Provider struct {
	keyOrder    []rune
	ngrams      []string
	transitions map[rune][]engine.Candidate
}

// sortedByCount returns m's keys, most frequent first, ties broken alphabetically.
func sortedByCount(m map[string]int) []string {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(m[b], m[a]), cmp.Compare(a, b))
	})
	return keys
}

func New() (*Provider, error) {
	var c gen.Counts
	err := json.Unmarshal(corpusJSON, &c)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling JSON: %w", err)
	}

	if len(c.Letters) != 26 {
		return nil, fmt.Errorf("expected 26 letters, got %d", len(c.Letters))
	}

	var keyOrder []rune
	for _, s := range sortedByCount(c.Letters) {
		keyOrder = append(keyOrder, []rune(s)[0])
	}

	ngrams := sortedByCount(c.Bigrams)

	// construct transitions map
	transitions := make(map[rune][]engine.Candidate)
	for s, n := range c.Bigrams {
		r := []rune(s)

		transitions[r[0]] = append(
			transitions[r[0]],
			engine.Candidate{Char: r[1], Freq: float64(n)},
		)
	}

	// normalise each transition list frequency
	for _, cands := range transitions {
		var sum float64
		for _, c := range cands {
			sum += c.Freq
		}
		for i := range cands {
			cands[i].Freq /= sum
		}
	}

	return &Provider{keyOrder: keyOrder, ngrams: ngrams, transitions: transitions}, nil
}

func (p *Provider) KeyOrder() []rune            { return p.keyOrder }
func (p *Provider) NgramsByFrequency() []string { return p.ngrams }
func (p *Provider) Transitions(context string) []engine.Candidate {
	if context == "" {
		return nil
	}
	return p.transitions[[]rune(context)[0]]
}
