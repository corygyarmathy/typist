package engine

import (
	"encoding/json"
	"fmt"
	"time"
)

// This file holds the engine's vocabulary: the domain types and the one
// interface it consumes. Behaviour lives in engine.go, scoring.go,
// progression.go and generate.go; nothing here has a method or a body.

// An item is the unit of competency. There are two kinds:
// - Keys - single runes (`e`, `t`, `;`, ...).
// - Ngrams - short character sequences (strings) that occur in the language
//   (`th`, `ing`, `he`, ...).
//
// Keys and ngrams are scored with the same machinery. The only differences are
// how they are unlocked and that an ngram is only ever active if all of its
// constituent keys are unlocked.

// ItemScore carries a smoothed competency estimate plus enough metadata to
// compute confidence and recency-decay for each item (keys, ngrams):
// - Score is what selection and unlocking read.
// - Samples expresses confidence: a high score off three keystrokes is not
// trustworthy.
// - LastPracticed enables decaying an item's effective score at read time so
// neglected items resurface.
//
// The json tags are load-bearing: ItemScore is the value type of both maps in
// the persisted competency document (docs/schema.md), so renaming a field here
// is a data migration, not a refactor.
type ItemScore struct {
	Score         float64   `json:"score"`          // smoothed competency [0,1]
	Samples       int       `json:"samples"`        // keystrokes observed; confidence
	LastPracticed time.Time `json:"last_practiced"` // for recency decay
}

// CompetencyState is a snapshot of a single user's typing competency at a
// point in time: per-key and per-ngram score state, the current ngram tier,
// and the tool-managed speed target.
//
// The two maps do NOT mean the same thing, despite their matching shapes:
//
//   - Keys is the unlock set. Presence means unlocked; there is no separate
//     set of unlocked keys - membership is the unlock, and nextKeyToUnlock is
//     the only thing that inserts.
//   - Ngrams is a score cache, not an unlock set. Which ngrams are in scope is
//     derived on read by activeNgrams from (NgramTier, unlocked keys); a
//     missing entry just means "in scope but never practised", which scores as
//     the zero ItemScore. Presence therefore means practised, not available.
//
// The asymmetry is deliberate: keys are a small, fixed, totally-ordered
// curriculum where being unlocked is the interesting state, whereas ngram
// availability is a function of the key set and so cannot drift out of sync
// with it if it is never stored.
//
// This is the JSONB document persisted per user (see docs/schema.md); it maps
// 1:1 to the engine's working type. That 1:1-ness is a deliberate,
// ADR-0009-blessed coupling and it has a price: the json tags on this type and
// on ItemScore are the persisted schema, so renaming a field is a migration
// concern rather than a local rename.
type CompetencyState struct {
	Keys      KeyScores            `json:"keys"`
	Ngrams    map[string]ItemScore `json:"ngrams"`
	NgramTier int                  `json:"ngram_tier"` // how many of the frequency-ranked ngrams are in scope
	TargetWPM int                  `json:"target_wpm"` // tool-managed speed threshold; see ADR 0012
}

// KeyScores is CompetencyState.Keys: the unlock set, keyed by the key itself.
//
// It is a named type solely so it can carry its own wire format. A rune is an
// int32, and encoding/json writes integer map keys as their quoted decimal, so
// the untyped map[rune]ItemScore persists key 'e' as "101" - legal JSON, and
// nothing like the document docs/schema.md specifies. The two methods below
// translate at the boundary and leave rune as the engine's vocabulary
// everywhere else; because a named map type is assignable to and from the
// unnamed one, no existing call site, literal or maps.Clone changed.
//
// The alternative extension point is a `type Key rune` with a MarshalText
// method, which json honours for map keys. Rejected here only because it wants
// to propagate through Corpus.KeyOrder, Candidate.Char and Result.Keys to avoid
// a rune/Key split, which is a package-wide vocabulary change made for a
// serialisation reason.
type KeyScores map[rune]ItemScore

// MarshalJSON writes the map keyed by the character itself ("e"), not by its
// code point.
//
// The receiver is a value, not a pointer, deliberately: with a pointer receiver
// the method would be absent from KeyScores' method set, and marshalling a
// CompetencyState value - which is how every caller holds one - would silently
// fall back to the integer-key encoding. The failure would be invisible until
// something read the persisted document. (Flipping the receiver here happens to
// stop compiling on the len below; that is luck, not protection - the same
// mistake in a body that compiles is silent.)
func (ks KeyScores) MarshalJSON() ([]byte, error) {
	// Always allocate, so a nil map marshals as {} rather than null: the
	// document in docs/schema.md always has a "keys" object, and a fresh state
	// that never round-trips cleanly is a bad thing to persist.
	m := make(map[string]ItemScore, len(ks))
	for r, score := range ks {
		m[string(r)] = score // rune -> its UTF-8 encoding, not its digits
	}
	return json.Marshal(m)
}

// UnmarshalJSON is the inverse, and rejects any key that is not exactly one
// character. Without that check "th" would decode as 't' and silently join the
// unlock set as a key the user never earned.
func (ks *KeyScores) UnmarshalJSON(b []byte) error {
	// By convention an Unmarshaler treats null as a no-op, so that decoding a
	// document with no "keys" leaves the field as the caller had it.
	if string(b) == "null" {
		return nil
	}

	var m map[string]ItemScore
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("decoding competency keys: %w", err)
	}

	out := make(KeyScores, len(m))
	for s, score := range m {
		r := []rune(s)
		if len(r) != 1 {
			return fmt.Errorf("competency key %q is not a single character", s)
		}
		out[r[0]] = score
	}
	*ks = out

	return nil
}

// The client aggregates per-item stats during a lesson and submits a summary.
// It does not ship or store raw keystrokes.

// Observation is a per-item, per-lesson measurement used to form the Result
// submitted to the engine.
type Observation struct {
	Attempts    int     // times this item was typed in the lesson
	Errors      int     // of those, how many were wrong
	TotalMillis float64 // cumulative time across attempts
}

// Result is a per-lesson submission of an Observation per item.
type Result struct {
	Keys   map[rune]Observation
	Ngrams map[string]Observation
}

// Lesson is a generated practice prompt: 10-15 english-like words built
// from the currently-unlocked keys and ngrams, weighted toward weak areas.
type Lesson struct {
	Words   []string // 10-15 generated words
	Targets []string // items this lesson was built to exercise (telemetry)
}

// Candidate is one possible next character in the transition graph, paired
// with the base frequency of the ngram it would form.
//
// It is declared here rather than in internal/corpus because it is part of the
// Corpus interface's vocabulary: a consumer-owned interface must own its
// signatures whole, since the type a method returns is as much a part of the
// contract as the method's name. Keeping it here is what lets `engine` build
// and test against a hand-written fake Corpus with internal/corpus absent
// entirely (the engine-first build, phase-3 plan Decision 2). The implementer
// imports this package to speak its vocabulary, so the dependency arrow points
// at the abstraction rather than away from it.
type Candidate struct {
	Char rune
	Freq float64
}

// Corpus is the read-only language data the engine consumes: a frequency order
// over keys for unlocking, a frequency-ranked ngram list that defines the
// tiers, and the transition graph the lesson generator walks.
//
// The engine declares it and receives it as a parameter; internal/corpus owns
// the data and implements it. The consumer owns the interface, which is what
// keeps the dependency pointing downward - `engine` never imports `corpus`.
type Corpus interface {
	KeyOrder() []rune                       // frequency order for unlocking
	NgramsByFrequency() []string            // frequency-ranked; defines tiers
	Transitions(context string) []Candidate // for the generator
}
