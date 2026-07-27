# ADR 0013: Corpus as embedded, generated statistical data

- **Status:** Accepted
- **Date:** 2026-06-22
- **Related Artefacts**:
  - [Engine Design Doc](/docs/engine.md)
  - [ADR 0014](/docs/adr/0014-engine-as-library-state-follows-identity.md)
  - [`Schema.md`](/docs/schema.md)

## Context

The adaptive engine depends on a `Corpus` (see the [engine design doc](/docs/engine.md)): a frequency order over letters, a frequency-ranked list of ngrams, and a transition graph for pseudo-word generation. It was undecided whether the corpus data would be stored in the database or not.

A second point is worth settling explicitly because it removes a worry about feasibility: the corpus is training data for _generation_, not a dictionary that lessons are drawn from. The generator synthesises english-like pseudo-words by sampling the transition graph under constraints (only unlocked keys, weighted toward weak items); it never selects whole words from a list. There is therefore no "the wordlist must contain a word that satisfies this lesson" requirement - the generator cannot starve, which is the whole reason it is preferred over a real-word dictionary filter (see the [engine design doc](/docs/engine.md)).

## Decision

The corpus is **static reference data, derived from a source corpus by a committed generator and embedded in the binary** - not a persisted bounded context.

- **Source.** A small, committed set of public-domain English prose - Project Gutenberg texts whose copyright has expired. Everything the engine needs is derived from it: letter frequencies (`KeyOrder`, `StartingKeys`), bigram and trigram frequencies (`NgramsByFrequency`), and the context -> next-character transition graph (`Transitions`), computed by walking the text token-by-token with explicit word-boundary markers.
- **Generation.** A small committed tool (`cmd/corpusgen`, or `internal/corpus/gen`) reads the source list and emits a generated artifact. The source list, the generator, and the generated artifact are all committed; `make corpus` regenerates. That triple is the provenance record.
- **Storage.** The generated artifact is embedded with `go:embed` (a JSON file for inspectability, or a generated `.go`). `internal/corpus` exposes the `Corpus` interface over the embedded data and owns no database code; the scaffolded `handler.go` / `repository.go` are dropped when the package is implemented.
- **Dataset and licence.** Public-domain English works from [Project Gutenberg](https://www.gutenberg.org). Only the public-domain text is consumed: the generator strips Project Gutenberg's header/footer boilerplate and trademark references - the part their licence actually governs - leaving content that is itself out of copyright, so the embedded corpus carries no attribution or redistribution obligation. The specific titles are listed alongside the generator in `internal/corpus`'s source directory.
- **Validation.** The generator's output is checked against a published reference ([Norvig's Mayzner letter/ngram analysis](https://norvig.com/mayzner.html)) within a tolerance, so the derived frequencies are demonstrably representative of English rather than an artefact of the chosen titles. This also bounds the scope: a handful of texts plus a tolerance test, not a corpus-engineering exercise.

## Consequences

**Positive**

- The engine stays pure and its `Corpus` dependency is satisfied in-process with zero I/O, which is exactly what lets the engine run both behind the API and in the offline / anonymous client (see: [ADR 0014](/docs/adr/0014-engine-as-library-state-follows-identity.md)).
- Bigrams-vs-trigrams etc. becomes a generator flag rather than a redesign: the tool emits both; the engine chooses.
- A test can assert the generated letter/ngram frequencies match a published reference (e.g. [Norvig's Mayzner letter-ngram analysis](https://norvig.com/mayzner.html)), demonstrating the corpus was validated rather than hand-waved.

**Negative**

- A generated artifact must be regenerated and re-committed when the source or the generator changes. Mitigated by `make corpus` and by committing all three pieces, so the output is reproducible.
- Embedding grows the binary by the artifact's size. Negligible for a bigram/trigram table.

## Alternatives considered

- **Corpus tables in Postgres**. Rejected: the data is read-only, identical for every user and every deployment, and needed in-process by a pure engine. A database table adds a migration, a load path, and a query layer for something that never changes per user - and it would make the offline / anonymous client (refer: [ADR 0014](/docs/adr/0014-engine-as-library-state-follows-identity.md)) impossible without a database.
- **A real-word dictionary as the primary lesson source.** Rejected for v1: it starves in the early game when only a few keys are unlocked, and it does not unify with the ngram competency model. Retained as a possible late-game variety source layered on top of the generator.
- **Hand-authored frequency tables.** Rejected: unverifiable, laborious, and a liability next to "derived from a known corpus by a committed tool."
