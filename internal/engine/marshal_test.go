package engine

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// These tests guard the one place a pure engine type meets a persisted shape.
// The competency document in docs/schema.md IS this struct (ADR 0009), so the
// json tags are a schema: nothing else in the codebase would notice if they
// drifted, because the handler decodes the stored bytes into an untyped
// openapi.Competency (map[string]any) and hands them straight to the client.

// schemaDocExample is the competency document copied verbatim from
// docs/schema.md. Restating it here rather than deriving it is the point: the
// test fails if the engine's encoding drifts from what the doc promises, which
// a self-consistent marshal/unmarshal round-trip could never catch.
const schemaDocExample = `{
  "keys": {
    "e": {
      "score": 0.91,
      "samples": 240,
      "last_practiced": "2026-06-16T09:00:00Z"
    },
    "t": {
      "score": 0.74,
      "samples": 180,
      "last_practiced": "2026-06-16T09:00:00Z"
    }
  },
  "ngrams": {
    "th": {
      "score": 0.62,
      "samples": 80,
      "last_practiced": "2026-06-16T09:00:00Z"
    }
  },
  "ngram_tier": 12,
  "target_wpm": 40
}`

func TestCompetencyState_MarshalsToSchemaDocument(t *testing.T) {
	practiced := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)

	state := CompetencyState{
		Keys: KeyScores{
			'e': {Score: 0.91, Samples: 240, LastPracticed: practiced},
			't': {Score: 0.74, Samples: 180, LastPracticed: practiced},
		},
		Ngrams:    map[string]ItemScore{"th": {Score: 0.62, Samples: 80, LastPracticed: practiced}},
		NgramTier: 12,
		TargetWPM: 40,
	}

	got, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshalling state: %v", err)
	}

	// Compared as decoded values, not as bytes: field order and whitespace are
	// not part of the JSON contract, so a struct field reordering should not
	// fail this test. The compacted forms are only printed on failure, where a
	// textual diff is what a reader actually wants.
	if !sameJSON(t, got, []byte(schemaDocExample)) {
		t.Errorf("marshalled state does not match docs/schema.md\n got: %s\nwant: %s",
			compact(t, got), compact(t, []byte(schemaDocExample)))
	}
}

// The bug this exists to catch: rune is int32, and encoding/json writes integer
// map keys as their quoted decimal, so an untagged map[rune]ItemScore persists
// key 'e' as "101". Legal JSON, unreadable document, and silently wrong.
func TestCompetencyState_KeysAreCharactersNotCodePoints(t *testing.T) {
	state := CompetencyState{Keys: KeyScores{'e': {Score: 0.5}}}

	got, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshalling state: %v", err)
	}
	if !bytes.Contains(got, []byte(`"e":`)) {
		t.Errorf("key 'e' not encoded as its character: %s", got)
	}
	if bytes.Contains(got, []byte(`"101":`)) {
		t.Errorf("key 'e' encoded as its code point: %s", got)
	}
}

func TestCompetencyState_RoundTrip(t *testing.T) {
	c := fakeCorpus{
		keyOrder:          []rune("etaoinshr"),
		ngramsByFrequency: []string{"th", "or", "eo", "ur"},
	}

	// The two states a caller can actually hold: the one InitialCompetency
	// hands to the registration transaction, and one that has been through
	// ApplyResult - which is the only one with practiced ngrams and non-zero
	// timestamps in it.
	practiced, _, res := engineFixturesFor(testNow, []rune("theours"))

	cases := map[string]CompetencyState{
		"initial":   InitialCompetency(c),
		"practiced": ApplyResult(practiced, c, res, testNow),
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(want)
			if err != nil {
				t.Fatalf("marshalling state: %v", err)
			}

			var got CompetencyState
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("unmarshalling state: %v", err)
			}

			// DeepEqual is safe here only because every timestamp in these
			// fixtures comes from time.Date. A state timestamped from
			// time.Now() carries a monotonic clock reading that marshalling
			// strips, so it would compare unequal to itself after a round
			// trip - which is why persisted state must never be compared to
			// its in-memory original with == or DeepEqual.
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip changed the state\n got: %+v\nwant: %+v", got, want)
			}
		})
	}
}

// A key wider than one character would decode to its first rune and silently
// join the unlock set as a key the user never earned, so it has to be an error.
func TestKeyScores_UnmarshalRejectsMultiCharacterKey(t *testing.T) {
	var got CompetencyState

	err := json.Unmarshal([]byte(`{"keys":{"th":{"score":0.5}}}`), &got)
	if err == nil {
		t.Fatalf("multi-character key accepted, decoded to %v", got.Keys)
	}
}

// A nil Keys map is what a zero CompetencyState has, and the document in
// docs/schema.md always carries a "keys" object - so nil normalises to {},
// not to null.
func TestKeyScores_NilMarshalsAsEmptyObject(t *testing.T) {
	got, err := json.Marshal(CompetencyState{})
	if err != nil {
		t.Fatalf("marshalling zero state: %v", err)
	}
	if !bytes.Contains(got, []byte(`"keys":{}`)) {
		t.Errorf("nil Keys did not marshal as an empty object: %s", got)
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()

	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("decoding %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("decoding %s: %v", b, err)
	}

	return reflect.DeepEqual(av, bv)
}

func compact(t *testing.T, b []byte) string {
	t.Helper()

	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		t.Fatalf("compacting %s: %v", b, err)
	}

	return out.String()
}
