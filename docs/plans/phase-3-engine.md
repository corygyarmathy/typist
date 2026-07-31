# Phase 3 implementation plan - the corpus + engine (the brain)

> A granular, session-to-session build plan for phase 3 of [`../roadmap.md`](../roadmap.md). The roadmap says _what_ phase 3 is and _when_ it's done; the [engine design doc](../engine.md) is the full spec (every formula, constant, and signature); this says _in what order we build it_, _how to approach designing it_, and _why each decision was made_. It is a living plan - update it as we go.

## How this phase is different (read first)

Phases 1-2 had two crutches this one doesn't, and that changes how we work:

1. **No reference branch.** Phase 2 had `ai-reference-phase-2` to reimplement top-down. Phase 3 has no reference - but it has something better: [`engine.md`](../engine.md) is an unusually complete spec. Every struct, formula, threshold, and function signature is already written down. So the _translation_ (spec → Go) is well-scaffolded; the _learning_ is in **how you decompose and structure** that translation, and in the design judgment each step demands. That judgment is what this plan front-loads.

2. **No HTTP edge.** Phase 2's driver was "curl a route, get a 501, let the failing call pull the next file into existence" - outside-in from the network boundary. This package has no network boundary. It is pure functions: `state in → state out`. The equivalent driver here is **the test**: a pure function's only caller is its test, so the failing test is what pulls the implementation into existence. Same red→green discipline, different outermost layer. See [How to approach pure domain logic](#how-to-approach-pure-domain-logic).

This is your first phase of genuine _application logic_ - the part that makes the app do something interesting - so the plan spends real space on **how to think about the design**, not just the steps.

## Where we are

- Branch to build on: `main` (phases 1-2 complete: config, logging, pgx pool, migrations, auth vertical, the `oapi-codegen` wiring, `GET /progress` behind the auth middleware).
- **Phase 3 is pure and parallelisable** - it imports nothing from the server, and the server barely imports it yet. The two seams where the server currently fakes the engine's absence:
  - `internal/progress/service.go:26` - `initialCompetency := []byte(`{"version":0}`)` with `// TODO(phase-3): replace with engine.InitialCompetency()`.
  - `cmd/server/main.go:38` - `// TODO(phase-3): construct the engine`.
- **Scaffolding already exists** as stubs with `TODO(phase-3)` markers - we fill these in, we don't create the packages from scratch:
  - `internal/engine/engine.go` - _step 1 complete:_ now holds the package doc, the constants block, and the two entry-point signatures as a TODO comment. The domain types moved to `internal/engine/types.go` (see step 1).
  - `internal/engine/types.go` - _added in step 1:_ the engine's vocabulary - `ItemScore`, `CompetencyState`, `Observation`, `Result`, `Lesson`, `Candidate`, and the `Corpus` interface.
  - `internal/engine/scoring.go` - _step 2 complete:_ `instant`, `updateScore` and `decayedScore`, with `scoring_test.go` covering the four invariants below. The threshold constants it originally listed live in `engine.go`'s block and are consumed by step 3, not here.
  - `internal/engine/engine_test.go` - a skipped placeholder test and a list of test categories.
  - `internal/corpus/corpus.go` - `Provider` an empty struct with a TODO to `go:embed` the generated artifact and implement four methods. (`Candidate` used to live here; step 1 moved it to `engine`.)
- **`cmd/corpusgen` does not exist yet.** It is created in this phase (the corpus track, below).

> **Renamed `internal/adaptive` → `internal/engine`, and `docs/adaptive-engine.md` → `docs/engine.md`** _(Cory, 2026-07-27)_. `adaptive` was a modifier with no noun - an adjective standing alone - and in Go the package name is read joined to the identifier at every call site, so `adaptive.CompetencyState` attached the adjective to the wrong noun. `engine` is the generic-noun failure in the abstract, but two things settle it here: the repo's own ubiquitous language already calls this thing "the engine" everywhere it is written in prose ([ADR 0014](../adr/0014-engine-as-library-state-follows-identity.md) is _engine-as-library_, `AGENTS.md`, the roadmap, this plan's filename), so the package name was the only remaining synonym; and `internal/` bounds the ambiguity - "engine of what" only bites when there could be two, and there is exactly one, unimportable from outside the module. `engine.CompetencyState` / `engine.ApplyResult` read noun-noun. Runner-up was `tutor`, the most self-describing option, rejected for introducing a fifth word for a concept the docs had already settled on. **"Adaptive" survives as prose** - it is a good adjective for the product (`README.md`, `flake.nix`, the OpenAPI description) and a bad one for an identifier. Not an ADR: a package rename has no cross-cutting opaque fork to record, so the living plan is the right vehicle.

## Definition of done (from the roadmap)

> Unit + property tests pass (e.g. `testing/quick`: a generated lesson contains only unlocked keys) **and** the simulated-user harness drives a "good" and a "struggling" learner through the alphabet in a bounded number of lessons.

Concretely, phase 3 ships:

- [ ] The engine types filled in (`CompetencyState`, `ItemScore`, `Observation`, `Result`, `Lesson`) and the `Corpus` interface the engine consumes.
- [ ] Scoring: instant score (accuracy-weighted), EMA update, read-time recency decay.
- [ ] Progression: key unlocking, ngram-tier advance (thin - see decisions), target-WPM raise, the key→ngram phase derivation.
- [ ] `ApplyResult` - folds a `Result` into state and applies at most one unlock / one tier advance / one target raise, in the spec's order.
- [ ] `NextLesson` - the weighted random walk over the corpus transition graph, restricted to unlocked keys, biased toward weak items.
- [ ] `engine.InitialCompetency()` and JSON marshaling that matches the `competency` document in [`../schema.md`](../schema.md); both server seams closed.
- [ ] `cmd/corpusgen` + the embedded artifact + `corpus.Provider`, with a **frequency-validation test** against the [Norvig/Mayzner](https://norvig.com/mayzner.html) reference.
- [ ] Unit + property tests at every layer, and the **simulated-user harness** proving bounded convergence for a good and a struggling learner.

## How to approach pure domain logic

You said you don't know where to start. Here is the general method, then the specific starting point. These principles transfer to any domain-logic package, not just this one - that's the point of writing them down.

1. **Read the spec as a dependency graph, not front-to-back.** Find the _leaves_ (things that depend on nothing) and the _root_ (the public contract). Here the root is the two entry points `NextLesson` / `ApplyResult`; the leaves are the small pure calculations - `instant`, the EMA update, `decayedScore`. **Build leaves → root.** Every step then has something concrete to test against, and the root falls out as composition of pieces you already trust. (This is the pure-function analogue of phase 2's outside-in: there, each layer pulled in the one below; here, each function composes the ones beneath it.)

2. **Types before behaviour - make the shape first.** Translate the spec's structs into Go types before writing a line of logic. Types are cheap, they're the vocabulary every function speaks, and pinning them down (`map[rune]ItemScore`, not `[]KeyScore`) forces you to understand the domain first. Where you can, make illegal states hard to represent.

3. **The test is your outside-in driver.** With no network edge, write the test first (or alongside). A test of a pure function is a runnable restatement of its signature and its spec: given this input, assert this output. It's what turns a signature + TODO list into working code. Start each step by writing the assertion you want to be true.

4. **Decompose a big transformation into _named predicates_.** `ApplyResult` is tempting to write as one 60-line function. Don't. Notice the spec already names its sub-decisions: _update the scores_, _should a key unlock?_, _should the tier advance?_, _should the target rise?_ Each becomes a small pure function you can test in isolation (`nextKeyToUnlock(...) (rune, bool)`), and `ApplyResult` becomes a short, readable sequence of them. The design lesson: **when a function makes several decisions, give each decision a name and a test.**

5. **Push impurity to the edges.** Never call `time.Now()` or touch a package-global RNG _inside_ the engine. Take `now time.Time` and `r *rand.Rand` as parameters (the signatures already do). The _why_ is testability: injected time makes decay deterministic; an injected seeded RNG makes "weak keys appear more often" an assertion instead of a hope.

6. **Depend on an interface you own, so you can fake it.** The engine _defines_ `Corpus` and _consumes_ it; `internal/corpus` implements it. The dependency points toward the abstraction the engine controls. This is exactly what lets us do the engine-first build you chose: the whole engine can be built and tested against a 5-key hand-written fake corpus, with the real generated data swapped in only at the end.

7. **Constants are guesses in one visible block.** Keep every tunable in a single `const` block (the spec's "Tunable constants" table). Treat the numbers as hypotheses to be validated by the harness, not truths - the harness _is_ how you'll find out whether 0.85 and `decayTau`=7d actually produce sane progression.

8. **The harness is the acceptance test of the whole design.** It's not an afterthought bolted on at the end - it's the thing that tells you the constants and the progression rules cohere into a system that _converges_ instead of stalling or racing. Design it in your head early (a "virtual typist" that turns intended text into a plausible `Result`), even though you build it last, because knowing it's coming keeps every layer honest and injectable.

**Where to start:** step 1 (types), then step 2 (scoring). Scoring is the ideal first real code - it's the smallest leaf, its formulas are fully specified in [`engine.md` §Scoring](../engine.md#scoring), and its tests are crisp and satisfying ("perfect-but-slow beats fast-but-sloppy"). You'll get a green test fast, which is the point.

## Decisions (locked in for phase 3)

### 1. Constants as a package-level `const` block, not an injected `Params` struct - _chosen (Cory, 2026-07-23)_

A single `const` block, exactly as the spec's table lists it. **Not** a `Params` struct threaded through the signatures.

> _Corrected during step 1:_ this decision originally hedged with "plus a couple of `var`s for the non-const `time.Duration`s". That turned out to be unnecessary - `time.Hour` is itself a typed constant, so `7 * 24 * time.Hour` is a constant expression and `decayTau` is a perfectly legal `const`. The whole block is one `const`, no `var`s. (Names are the Go-idiomatic unexported lowerCamel of the spec's table: `wAccuracy`, `minSamples`, `decayTau`, ... - Go has no SCREAMING*SNAKE convention, and these are engine internals, not the package's contract.) Rationale, in order of weight: (a) the phase-3 done-condition only ever runs the harness against \_one* constant set - programmatic sweeping of multiple sets is a spec _bonus_ ("doubles as a tuning tool"), not part of the DoD, so the need that would justify the struct doesn't arrive inside this phase; (b) it matches the spec's stated "package-level pure functions (not methods on a struct)," keeping code and doc telling one story; (c) the promotion is a cheap, compiler-driven refactor precisely because the package is pure, so deferring it costs almost nothing.

> **Future option.** Promote the block to an injected `Params` (a value the engine funcs hang off, or a `DefaultParams()` + a field on an `Engine`) **when the harness needs to compare multiple constant sets in a single run.** That is the concrete revisit trigger; until it fires, the block stays. Record the reasoning as a short comment on the block, not an ADR (it fails the reach bar - it's a local structuring choice).

### 2. Engine-first, against a hand-written fake `Corpus` - _chosen (Cory, 2026-07-23)_

Build the interesting logic (types → scoring → progression → generation → harness) against a tiny fake `Corpus` (a handful of keys, a hand-specified transition table), then build `cmd/corpusgen` + the embedded artifact and swap the real `Provider` in behind the same interface. This keeps the data-plumbing chore off the critical path and showcases the interface-driven design - the swap should require _no_ change to the engine, which is the proof the boundary is real.

### 3. Bigrams only; ngram-tier progression stays thin - _from the roadmap_

Per the roadmap and the design doc's open questions: **bigrams only** in phase 3 (the corpus generator may emit trigrams too, but the engine ignores them for now). Keep ngram _scoring_ fully in (an ngram is a first-class scored item - that's the whole contribution), but the ngram-**tier** progression axis can be thin: implement the `advance tier` predicate and the "active iff within tier and all keys unlocked" rule, but don't over-invest in tier tuning. Keys are the axis that must work end-to-end for the DoD (all 26 unlock); tiers deepen later. Note the choice in the package doc / README; it's already covered by the design doc's open questions, so **no ADR**.

### 4. Key-introduction order: pure frequency - _from the spec_

Unlock in `corpus.KeyOrder()` (frequency order), the keybr-faithful default. A pedagogical order (home-row-first) is a documented open question, not a phase-3 choice.

## The build order (inside-out, test-first)

Each step names **what you write** (the signature + a TODO list - your shape to fill in, not filled-in code), **the design question** to wrestle with (this is the learning), and **how you prove it** (the test / checkpoint). Build leaves → root. Expect to revisit a lower step when an upper one reveals a missing helper - that's the method working, not a mistake.

The formulas themselves live in [`engine.md`](../engine.md) - when a step says "translate §Scoring," the arithmetic is _there_; your work is turning it into well-structured, tested Go, which is where the understanding is built. I've deliberately not pre-written the bodies.

### Step 1 - Types & the constants block (`engine.go`, a new `types.go` if it reads better)

**Write:** fill the stub structs and add the rest, per [§Core model](../engine.md#core-model):

```go
type ItemScore struct {
    Score         float64   // smoothed competency [0,1]
    Samples       int       // keystrokes observed; confidence
    LastPracticed time.Time // for recency decay
}
type CompetencyState struct {
    Keys      map[rune]ItemScore
    Ngrams    map[string]ItemScore
    NgramTier int
    TargetWPM int
}
type Observation struct{ Attempts, Errors int; TotalMillis float64 }
type Result struct {
    Keys   map[rune]Observation
    Ngrams map[string]Observation
}
type Lesson struct{ Words, Targets []string }

type Corpus interface {
    StartingKeys() int
    KeyOrder() []rune
    NgramsByFrequency() []string
    Transitions(context string) []Candidate
}

const ( /* wAccuracy, wSpeed, alpha, thresholds, ... — the spec's table */ )
```

**The design question:** _where does `Candidate` live?_ The `Corpus` interface (in `engine`) returns `[]Candidate`, but the engine must **not** import `corpus`. So the shared value type can't sit only in `corpus`. Two options: (a) move `Candidate` into `engine` next to the interface, and have `corpus` (the implementer) import `engine` to speak its vocabulary; (b) keep it in `corpus` and accept the import direction that implies. Reason it out - which keeps the dependency arrow pointing the way principle 6 wants? This is a small but real decision about dependency direction; make it deliberately.

> **Decided (a) - `Candidate` lives in `engine`** _(Cory, 2026-07-26)_. Three reasons, in increasing order of weight. (1) A method's return type is as much a part of an interface's contract as its name, so a consumer-owned interface must own its signatures whole - under (b) `engine` would only half-own `Corpus`. (2) It is what makes Decision 2 real: under (b), `internal/engine` could not compile without `internal/corpus`, and the hand-written fake corpus in the engine's own tests would have to import the real package just to name the type it returns - the "swap the Provider in behind the interface with no engine change" proof would evaporate. (3) Go forbids import cycles, so (b) is a dead end anyway: once `engine` imports `corpus`, `corpus` can never import `engine`, and the refactor happens later under duress instead of deliberately. The cost - `corpus` now depends on something "above" it - is the point: the implementer depends on the abstraction. Rationale recorded as a comment on `engine.Candidate` and on `corpus.Provider`, **not** an ADR (fails the reach bar, per the Documentation & tooling impact section).
>
> **Also decided: types split into `types.go`.** `types.go` holds the vocabulary (the five domain types, `Candidate`, and the `Corpus` interface); `engine.go` keeps the package doc, the constants block, and the two entry points. Done at step 1 rather than deferred, because step 4 puts `ApplyResult` in `engine.go` and the file would have been carrying two jobs by then. The constants stay in `engine.go` - they are behavioural tuning shared by every behaviour file in the package, not vocabulary, and `engine.go` is the package's behavioural root.

**Prove it:** nothing to test yet - types don't have behaviour. It compiles, and the two entry-point signatures now typecheck. Delete the `engine_test.go` `Skip` when step 2 gives you a real assertion.

### Step 2 - Scoring primitives (`scoring.go`) — _start here_

**Write** three small pure functions; translate [§Scoring](../engine.md#scoring):

```go
// instant score for one item's observation this lesson: accuracy-weighted, [0,1].
func instant(o Observation, targetWPM int) (score float64, ok bool)
// fold an observation into an item's stored score: EMA of instant, bump Samples,
// set LastPracticed. New item (Samples==0) ⇒ Score = instant.
func updateScore(prev ItemScore, o Observation, targetWPM int, now time.Time) (ItemScore, bool)
// effective score at read time: raw Score decayed by age since LastPracticed.
func decayedScore(s ItemScore, now time.Time) float64
```

> **Decided during the step: the two folding functions return `(value, ok)`, not a bare value** _(Cory, 2026-07-27)_. The three degenerate inputs (`Attempts == 0`, `TotalMillis == 0`, `targetWPM == 0`) are caller bugs, so the tempting shape was a guard that returns `0`. But `0` is a legal score, so that sentinel is indistinguishable from "typed abysmally" and folds silently into persisted state - and it invites the caller to ignore the problem, which is exactly what it did once during this step, letting a mistyped test fixture pass by comparing a real score against the sentinel. The bool makes the invalid case something a caller must handle or explicitly discard. On `!ok`, `updateScore` returns `prev` unchanged so that discarding it no-ops rather than erasing the item's history. The guard is kept even though `ApplyResult` should reject these upstream, because `targetWPM == 0` is integer division and would panic rather than merely return a wrong number - and a zero-valued `CompetencyState` has `TargetWPM: 0`. An earlier attempt logged the rejection via `slog`; that was removed as a violation of the package's no-I/O rule (see [`../engine.md`](../engine.md#scoring)) - the engine runs in-process inside the TUI, where the default handler writes over the render.

**The design question:** decay is applied _at read time_, never stored (the stored `Score` stays honest; decay is a lens). Internalise _why_ - it means no background job, and it's a pure function of two timestamps, so it's deterministic and trivially testable. Which functions should call `decayedScore` and which the raw `Score`? (Selection and unlocking read decayed; the EMA update reads raw.)

**Prove it** (`scoring_test.go`): a perfect-but-slow observation scores higher than a fast-but-error-laden one (accuracy weight dominates - this is the headline invariant); a brand-new item's score equals its `instant`; two identical items with different `LastPracticed` - the staler one has the lower `decayedScore`; each of the three degenerate inputs reports `!ok` from both folding functions. Write the first three as _relations between two calls_ rather than against hand-computed constants, so they survive retuning and stay sensitive to the thing under test - the headline one should go red if `wAccuracy` and `wSpeed` are swapped. **Checkpoint:** these green = the math core is trustworthy.

### Step 3 - Progression predicates (`progression.go`)

**Write** the decision functions, each named and independently testable; translate [§Progression](../engine.md#progression):

```go
func activeNgrams(s CompetencyState, c Corpus) []string          // within tier AND all keys unlocked
func nextKeyToUnlock(s CompetencyState, c Corpus, now time.Time) (rune, bool)
func shouldAdvanceNgramTier(s CompetencyState, c Corpus, now time.Time) bool
func shouldRaiseTarget(s CompetencyState, c Corpus, now time.Time) bool
func phaseIsNgrams(s CompetencyState, c Corpus, now time.Time) bool        // the soft key→ngram transition
```

**The design question:** every predicate is an "and over all items" gate with **two** conditions - a `decayedScore` threshold _and_ a `minSamples` floor. Why does the sample floor exist? (A lucky high score off three keystrokes must not unlock.) Getting the "for all unlocked keys" quantifier right - and unlocking exactly _one_ key at a time in `KeyOrder` - is the subtle part.

**Prove it** (`progression_test.go`): a state where all keys clear the score bar but one is under `minSamples` → no unlock; all clear both → exactly the next key in `KeyOrder` unlocks; target raise fires only when _all_ keys are unlocked and mean decayed score clears the bar.

### Step 4 - `ApplyResult` (`engine.go`)

**Write** the first entry point - composition of steps 2-3, in the spec's exact order:

```go
// Pure: returns a new state, never mutates s. now is injected.
func ApplyResult(s CompetencyState, res Result, now time.Time) CompetencyState
```

**TODO order** (from [§The two engine functions](../engine.md#the-two-engine-functions)): copy the state → fold every observed key and ngram via `updateScore` → then `nextKeyToUnlock` (apply at most one) → then `shouldAdvanceNgramTier` (at most one) → then `shouldRaiseTarget` (at most one step). Scoring _before_ unlocking is deliberate: a lesson's own result can earn the unlock it triggers.

**The design question:** _purity means you must not mutate the input maps_ - callers may still hold the old state. How do you copy `map[rune]ItemScore` cleanly (and cheaply enough)? And is there a case for a small internal mutable working copy that you return as the new state? Decide, and make the no-mutation guarantee something a test enforces.

**Prove it** (`engine_test.go`): a `Result` that pushes the last-needed key over both gates produces a state with the next key newly present (order-of-operations proof); passing the same input twice returns equal states and the _original_ input is unchanged (purity proof).

### Step 5 - `NextLesson` / generation (`generate.go` + `engine.go`) — the hard one

**Write** the second entry point and its helpers; translate [§Lesson generation](../engine.md#lesson-generation):

```go
func need(item string, s CompetencyState, now time.Time) float64 // 1-decayed; 1.0 for new active items
func weightedSample(cands []Candidate, weights []float64, r *rand.Rand) rune
// Pure: all randomness flows through r; decay computed from now.
func NextLesson(s CompetencyState, c Corpus, now time.Time, r *rand.Rand) Lesson
```

**TODO:** for each step of the walk, get `corpus.Transitions(context)`, drop candidates whose key isn't unlocked, weight each by `baseFreq * (1 + lambdaKey*need(key)) * (1 + lambdaNgram*need(ngram))` (with `lambdaNgram` scaled by phase), sample the next char through `r`; insert word boundaries to hit the length distribution; loop to 10-15 words; record high-`need` items in `Targets`.

**The design question:** this is the most open-ended step, so **build it in two passes.** Pass 1: ignore weakness entirely - just walk the transitions restricted to unlocked keys. That alone makes the headline invariant ("every character is an unlocked key") pass, and proves the walk terminates and produces words. Pass 2: layer the weighting on and prove weak items surface more often. Sub-problems to think through: how do you seed the walk's first context with only a few keys unlocked (and never starve)? How do you sample proportional to weights with an injected RNG (cumulative weights + one `r.Float64()`)?

**Prove it** (`generate_test.go`, property-based): `testing/quick` - for any valid state, every character of every generated word is an unlocked key (the roadmap's named invariant); across many _seeded_ runs, a low-`decayedScore` key appears more often than a high one; a freshly unlocked key shows up in the next lesson's `Targets`. Seed `r` so frequency assertions are deterministic.

### Step 6 - The simulated-user harness (the headline artifact)

**Write** a "virtual typist" and the convergence loop. This is a test (`harness_test.go`) for the DoD assertions; optionally also a tiny `cmd/` or `Example` if you later want to print tuning tables.

```go
// A profile turns intended text into a plausible Result: it fabricates
// Observations consistent with the attribution rules in the design doc,
// at a target accuracy and speed. "good" and "struggling" are two profiles.
func simulate(lesson Lesson, p profile, r *rand.Rand) Result
```

**TODO:** loop `NextLesson` → `simulate` → `ApplyResult`, feeding state forward; assert a **good** learner unlocks all 26 keys within a bounded lesson count; a **struggling** learner takes longer but stays bounded (or plateaus below - decide what "handled" means for them); assert the phase flips key→ngram once mean key `decayedScore` crosses `phaseThreshold`.

**The design question:** the harness is only as honest as the virtual typist. It must fabricate `Observation`s the way a real client would _measure_ them (the [attribution rules](../engine.md#client-observation-model): first-try errors, per-window ngram attribution, no spaces scored). If the harness cheats, the convergence proof is worthless. This is also where you'd feel the pain if you'd wanted programmatic constant-sweeping - note whether the Future-option trigger from Decision 1 has fired.

**Prove it:** the harness _is_ the proof - it's the roadmap's headline done-condition. **Checkpoint:** good + struggling learners both converge as asserted, deterministically under a fixed seed.

### Step 7 - `InitialCompetency()`, JSON marshaling, and closing the seams

**Write:**

```go
func InitialCompetency() CompetencyState // startingKeys most-frequent keys unlocked at zero score, NgramTier init, TargetWPM=40
```

**TODO:** add JSON tags to the engine types so `CompetencyState` marshals to the `competency` document in [`../schema.md`](../schema.md) exactly: `keys` / `ngrams` maps of `{score, samples, last_practiced}`, plus `ngram_tier` and `target_wpm`. Then close the two seams: `internal/progress/service.go:26` uses `json.Marshal(engine.InitialCompetency())` instead of `{"version":0}`; resolve `cmd/server/main.go:38` (with package-level funcs there's little to "construct" - mostly it's wiring `corpus.Provider` so the eventual `/lessons/next` in phase 4 has a corpus; keep this minimal and honest about what phase 3 actually needs). Remove the nolint directive in `types.go` if still present.

**The design question:** this is where the pure engine type meets the persisted shape. Coupling the engine struct's JSON tags to the DB document is a deliberate, ADR-0009-blessed choice (the JSONB doc _is_ the `CompetencyState`) - but it means a field rename in the engine is a migration concern. Worth a one-line comment at the type saying so.

**Prove it:** a round-trip test - `InitialCompetency()` → `json.Marshal` → assert it matches the schema-doc example's shape; unmarshal back → equal state. **Checkpoint:** register a new user against the real server → the persisted `competency` row is a valid initial `CompetencyState`, not `{"version":0}`.

## The corpus track (build after the engine, swap behind the interface)

Independent of the engine's logic; gated only by the frequency-validation test. Because the engine was built against a fake `Corpus`, this slots in behind the same interface with no engine change - that's the payoff of Decision 2.

- **C1 - `cmd/corpusgen`.** A committed offline generator ([ADR 0013](../adr/0013-corpus-as-embedded-generated-data.md)): read a source (the Norvig/Mayzner frequency data, or a text corpus you reduce), compute the letter frequency order, the frequency-ranked ngram list, and the `context → []Candidate` transition graph; emit `internal/corpus/data/corpus.json`. Stdlib only if you can manage it (reinforces the stdlib-first ADRs).
- **C2 - `corpus.Provider`.** `//go:embed data/corpus.json`, parse once at init, implement `StartingKeys` / `KeyOrder` / `NgramsByFrequency` / `Transitions`. Swap it for the fake in the harness/integration tests.
- **C3 - frequency-validation test.** Assert the embedded artifact's letter (and bigram) frequencies match the [Norvig/Mayzner](https://norvig.com/mayzner.html) reference within a tolerance. This is the test that says "the data is real English, not noise" - a small, high-signal portfolio artifact.

## Testing strategy

- **Unit (pure, no I/O):** `scoring_test.go`, `progression_test.go`, `engine_test.go` (ApplyResult) - table-driven, deterministic via injected `now`.
- **Property-based (`testing/quick`):** the selection invariants in step 5 - "any valid state ⇒ a lesson of only unlocked keys" is the canonical one the roadmap names.
- **Seeded statistical:** "weak keys appear more often than strong" - assert over many runs under a fixed seed, with a margin, not an exact count.
- **Harness (end-to-end, still pure):** the bounded-convergence and phase-flip proofs in step 6 - the DoD.
- **Corpus:** the C3 frequency-validation test.

No DB and no HTTP in this package's tests - if a test needs either, the logic has leaked out of the engine and should move to the service layer (phase 4).

## Documentation & tooling impact

- **`go.mod`:** expect **no new dependencies** - `math`, `math/rand`, `time`, `encoding/json`, `embed` are all stdlib. (A nice contrast with phase 2, which pulled in argon2/jwt; worth noting for the portfolio.) If a corpus source needs a parser, prefer stdlib first and justify anything more.
- **ADRs:** expect **none new.** The cross-cutting engine decisions are already recorded - [0012](../adr/0012-targets-set-by-tool-not-user.md) (targets by tool), [0013](../adr/0013-corpus-as-embedded-generated-data.md) (corpus embedded/generated), [0014](../adr/0014-engine-as-library-state-follows-identity.md) (engine as library), [0009](../adr/0009-json-serialised-data.md) (JSON competency doc). The engine's internal math is documented by [`engine.md`](../engine.md) (a design doc, the right vehicle) plus code comments. The constants-block choice, the `Candidate` placement, and bigrams-only all fail the ADR reach/opacity bars → package or code comments, not new numbers. This keeps the ADR index from sprawling, which is itself the correct call to be able to defend.
- **`internal/corpus/data/`:** new committed generated artifact (`corpus.json`) + the `go:embed`.
- **Makefile:** consider a `corpusgen` target that regenerates the artifact, so the generation step is reproducible and documented rather than a one-off.

## Open questions / seams for later

- **Trigrams** - deferred; the generator may emit them, the engine ignores them in phase 3 (Decision 3).
- **Velocity / anti-plateau generation** - explicitly deferred by the design doc (an online scalar, not a history table); sequence it after the harness exists.
- **Real-word dictionary** as a late-game variety source - out of v1 per [ADR 0013](../adr/0013-corpus-as-embedded-generated-data.md).
- **Key-introduction order** - frequency for now; pedagogical (home-row-first) is an open question.
- **Constants → `Params` struct** - promote only when the harness needs to sweep multiple sets in one run (Decision 1's trigger).
- **`cmd/server/main.go:38`** - phase 3 wires just enough corpus for the engine to be constructible; the real consumer (`GET /lessons/next`, the transactional `POST /sessions`) is phase 4.
