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
  - `internal/corpus/corpus.go` - _corpus track complete:_ now the package doc only. `Provider` and its `New()` constructor moved to `internal/corpus/provider.go`; the counting logic is `internal/corpus/gen`; validation is `frequency_test.go`. (`Candidate` used to live here; step 1 moved it to `engine`.)
- **`cmd/corpusgen`** - _created in this phase,_ with a `make corpus` target that regenerates `internal/corpus/data/corpus.json` from the committed sources.

> **Phase 3 is complete as of 2026-08-20.** Every definition-of-done item below is ticked, both server seams are closed, and `go test ./...` is green including the database-backed integration and e2e tests. The deferred items in [Open questions](#open-questions--seams-for-later) are deliberate carry-over, not unfinished work - several are measured defects with recorded triggers, and phase 4 is what will supply the feedback to settle them.

> **Renamed `internal/adaptive` → `internal/engine`, and `docs/adaptive-engine.md` → `docs/engine.md`** _(Cory, 2026-07-27)_. `adaptive` was a modifier with no noun - an adjective standing alone - and in Go the package name is read joined to the identifier at every call site, so `adaptive.CompetencyState` attached the adjective to the wrong noun. `engine` is the generic-noun failure in the abstract, but two things settle it here: the repo's own ubiquitous language already calls this thing "the engine" everywhere it is written in prose ([ADR 0014](../adr/0014-engine-as-library-state-follows-identity.md) is _engine-as-library_, `AGENTS.md`, the roadmap, this plan's filename), so the package name was the only remaining synonym; and `internal/` bounds the ambiguity - "engine of what" only bites when there could be two, and there is exactly one, unimportable from outside the module. `engine.CompetencyState` / `engine.ApplyResult` read noun-noun. Runner-up was `tutor`, the most self-describing option, rejected for introducing a fifth word for a concept the docs had already settled on. **"Adaptive" survives as prose** - it is a good adjective for the product (`README.md`, `flake.nix`, the OpenAPI description) and a bad one for an identifier. Not an ADR: a package rename has no cross-cutting opaque fork to record, so the living plan is the right vehicle.

## Definition of done (from the roadmap)

> Unit + property tests pass (e.g. `testing/quick`: a generated lesson contains only unlocked keys) **and** the simulated-user harness drives a "good" and a "struggling" learner through the alphabet in a bounded number of lessons.

Concretely, phase 3 ships:

- [x] The engine types filled in (`CompetencyState`, `ItemScore`, `Observation`, `Result`, `Lesson`) and the `Corpus` interface the engine consumes.
- [x] Scoring: instant score (accuracy-weighted), EMA update, read-time recency decay.
- [x] Progression: key unlocking, ngram-tier advance (thin - see decisions), target-WPM raise, the key→ngram phase derivation.
- [x] `ApplyResult` - folds a `Result` into state and applies at most one unlock / one tier advance / one target raise, in the spec's order.
- [x] `NextLesson` - the weighted random walk over the corpus transition graph, restricted to unlocked keys, biased toward weak items.
- [x] `engine.InitialCompetency()` and JSON marshaling that matches the `competency` document in [`../schema.md`](../schema.md); both server seams closed.
- [x] `cmd/corpusgen` + the embedded artifact + `corpus.Provider`, with a **frequency-validation test** against the [Norvig/Mayzner](https://norvig.com/mayzner.html) reference.
- [x] Unit + property tests at every layer, and the **simulated-user harness** proving bounded convergence for a good and a struggling learner.

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
func ApplyResult(s CompetencyState, c Corpus, res Result, now time.Time) CompetencyState
```

**TODO order** (from [§The two engine functions](../engine.md#the-two-engine-functions)): copy the state → fold every observed key and ngram via `updateScore` → then `nextKeyToUnlock` (apply at most one) → then `shouldAdvanceNgramTier` (at most one) → then `shouldRaiseTarget` (at most one step). Scoring _before_ unlocking is deliberate: a lesson's own result can earn the unlock it triggers.

**The design question:** _purity means you must not mutate the input maps_ - callers may still hold the old state. How do you copy `map[rune]ItemScore` cleanly (and cheaply enough)? And is there a case for a small internal mutable working copy that you return as the new state? Decide, and make the no-mutation guarantee something a test enforces.

> **Decided during the step: `ApplyResult` takes `c Corpus`** _(Cory, 2026-08-01)_. The signature this plan and [`engine.md`](../engine.md#the-two-engine-functions) originally specified had no corpus, and it cannot work: all three progression predicates from step 3 need one. Worth noticing _why_ the spec got it wrong - "fold a result into state" reads as self-contained, and the scoring half is, but unlocking is inherently a question about the ordered universe of keys, which the state alone does not know. State records what is unlocked; only the corpus records what is next. `c` sits second so both entry points read `(state, corpus, ...inputs, ...injected)`. The design doc was corrected to match.
>
> **Also decided: ngram scope stays a tier, not a second unlock set** _(Cory, 2026-08-01)_. The obvious symmetry - a `nextNgramToUnlock` mirroring `nextKeyToUnlock`, with presence in `CompetencyState.Ngrams` meaning unlocked - was considered and rejected. Under it, "which ngrams are available" becomes a fact that must be _maintained_: every key unlock potentially makes new ngrams typeable, so the stored set is stale until something reconciles it, and that reconciliation is a bug waiting to be written in a value that round-trips through JSONB on every submission. `activeNgrams` recomputes scope from `(NgramTier, unlocked keys)` on every read, so there is no stale state and nothing to reconcile - a derived set cannot be wrong. The two axes are also answering different questions: keys are a small fixed curriculum where _unlocked_ is the interesting state; ngrams are a long frequency-ranked tail where the interesting state is the score and the tier is just a cursor saying how deep we have gone. Consequence for `ApplyResult`: the ngram fold gates on `activeNgrams`, not on map presence, and a first observation of a newly-active ngram is what creates its entry - which is what makes `scoresOf`'s "a missing name yields the zero `ItemScore`" coherent rather than accidental. Recorded here and as a comment on `CompetencyState`, **not** an ADR (a local structuring choice; fails the reach bar).

**Prove it** (`engine_test.go`): a `Result` that pushes the last-needed key over both gates produces a state with the next key newly present (order-of-operations proof); passing the same input twice returns equal states and the _original_ input is unchanged (purity proof).

> **Two things the tests had to be built around** _(step 4)_. The purity test only has teeth if it mutates the returned state _through_ a map - reassigning a field on the returned struct can never reach the input, so that version of the test passes with the `maps.Clone` calls deleted. And the fixture's prior scores must sit inside the band one lesson can actually close: an EMA step moves at most `alpha` of the way to `instant`, so a key can only cross `unlockKeyThreshold` in one call from a prior score of at least `(unlockKeyThreshold - alpha) / (1 - alpha)` ≈ 0.786. A "just below the bar" fixture at 0.75 is unreachable by construction and no observation can rescue it. The derivation is written into the fixture, because it is the thing that will break the moment anyone retunes `alpha`.

### Step 5 - `NextLesson` / generation (`generate.go` + `engine.go`) — the hard one

**Write** the second entry point and its helpers; translate [§Lesson generation](../engine.md#lesson-generation):

```go
func need(item string, s CompetencyState, now time.Time) float64 // 1-decayed; 1.0 for new active items
func weightedSample(cands []Candidate, weights []float64, r *rand.Rand) rune
// Pure: all randomness flows through r; decay computed from now.
func NextLesson(s CompetencyState, c Corpus, now time.Time, r *rand.Rand) Lesson
```

**TODO:** for each step of the walk, get `corpus.Transitions(context)`, drop candidates whose key isn't unlocked, weight each by `baseFreq * (1 + lambdaKey*need(key)) * (1 + lambdaNgram*need(ngram))` (with the ngram lambda scaled by phase), sample the next char through `r`; insert word boundaries to hit the length distribution; loop to 10-15 words; record high-`need` items in `Targets`.

**The design question:** this is the most open-ended step, so **build it in two passes.** Pass 1: ignore weakness entirely - just walk the transitions restricted to unlocked keys. That alone makes the headline invariant ("every character is an unlocked key") pass, and proves the walk terminates and produces words. Pass 2: layer the weighting on and prove weak items surface more often. Sub-problems to think through: how do you seed the walk's first context with only a few keys unlocked (and never starve)? How do you sample proportional to weights with an injected RNG (cumulative weights + one `r.Float64()`)?

**Prove it** (`generate_test.go`, property-based): `testing/quick` - for any valid state, every character of every generated word is an unlocked key (the roadmap's named invariant); across many _seeded_ runs, a low-`decayedScore` key appears more often than a high one; a freshly unlocked key shows up in the next lesson's `Targets`. Seed `r` so frequency assertions are deterministic.

> Decided during the step:
>
> 1. pick split out of weightedSample — the deterministic core takes a draw instead of a \*rand.Rand. Reason: the strict > vs >= comparison only differs when draw is exactly 0, which is unreachable through r.Float64() (probability ~2⁻⁵³), so the boundary is untestable without the split.
> 2. Seed order comes from c.KeyOrder() filtered by s.Keys, never from ranging s.Keys. Go randomises map iteration, pick scans in slice order, so ranging the map makes output non-deterministic under a fixed seed. This is the first place in the engine where map order becomes observable — ApplyResult ranges maps too but never lets order reach its output.
> 3. Pass-1 seed weight is a uniform 1.0. KeyOrder gives order, not frequency, so there is no corpus frequency for a bare key. Becomes 1 + lambdaKey\*need(k) in pass 2.
> 4. Starvation ends the word, and sub-minWordLen words are accepted. Deferred to the step-6 harness to decide whether it bites — the fixture deliberately over-represents it.
> 5. A word start has no bigram, so no ngram factor. Worth writing down because need's length dispatch makes the naive need(context + string(c)) silently return the key's need at position 0, double-counting it. This is the one live trap waiting in pass 2.
> 6. New constants: minWordLen, maxWordLen; pass 2 adds a second ngram lambda (the table's 0.5→3.0 needs two identifiers, since phaseIsNgrams is a bool rather than a ramp).

> Decided during pass 2 (the weighting layer):
>
> 1. **The ngram factor is gated on `activeNgrams`** - the one place the code deliberately departs from [`engine.md`](../engine.md#lesson-generation)'s pseudocode, so expect to be asked about it. `need` is `1 - decayedScore`, and an out-of-tier ngram has no stored score, so it evaluates to a permanent 1.0: `ApplyResult` drops observations for out-of-scope ngrams, so nothing can ever lower it. Applying the factor unconditionally would mean every untracked bigram outranks a _mastered_ in-scope one forever, and `NgramTier` would stop influencing generation at all - a boost the engine is structurally unable to satisfy. Out of scope now multiplies by exactly 1.0. The design doc was corrected to match; `TestCandidates_NgramFactorGatedOnScope` is what pins it, by making two candidates differ _only_ in scope and asserting their weights differ only by base frequency.
> 2. **`lessonScope` - the per-lesson derived context.** Pass 2 gave the weighting five inputs that are constant for a whole lesson (state, corpus, the active-ngram set, the phase-scaled lambda, `now`), and threading all five through `nextWord` → `unlockedCandidates` pushed the latter to six parameters. They are now fields on an unexported `lessonScope` built once in `NextLesson`, with the generation helpers as methods. This is **not** Decision 1's `Params` struct and does not trip its revisit trigger: it holds no tunables, only values _derived_ from the arguments the entry points already take, and the entry points stay package-level pure functions. Hoisting is also what makes the gate in (1) affordable - `activeNgrams` walks the whole tier, and the walk asks about scope once per candidate per step.
> 3. **`Targets` is one ranking over keys _and_ ngrams**, not a merge of two separately-ranked lists, capped at `lessonTargets`. Two lists concatenated would mean the 5th-weakest key always outranks the weakest ngram, which defeats the point once the phase flips. Enumeration is `KeyOrder()` filtered by the unlock set, then the active ngrams in frequency order, followed by a **stable** sort on need - so ties break by corpus frequency and never by map iteration order. This is the second place map order would have become observable (see pass 1, decision 2), and the fixture makes it bite: its six keys all share a need, so every slot after the freshly-unlocked one is filled from ties.
> 4. **`weightedCandidate` flattened to `{char, weight}`.** It carried a whole `Candidate`, but sampling only ever reads the character, and pass 2 made the `Freq` field actively misleading - a seed has no corpus frequency (`KeyOrder` gives order, not frequency), and a mid-word candidate's frequency is already folded into `weight`. Deleting the field removed the last pass-1 placeholder.
> 5. New constants: `lambdaNgramKeyPhase`, `lambdaNgramNgramPhase`, `lessonTargets`. `activeNgramSet` moved next to `activeNgrams` in `progression.go` and is now shared with `ApplyResult`, which had built the same slice-plus-set pair inline.

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

> **Decided during the step - the harness's own shape** _(Cory, 2026-08-12)_:
>
> 1. **`harness_test.go` is an internal test (`package engine`), not `package engine_test`.** It asserts on `phaseIsNgrams`, `activeNgrams`, `need`, `decayedScore` and the constants, all unexported. `fake_test.go` set the precedent in step 2. The externally-visible contract is exercised by the entry points the harness already drives; nothing is gained by testing this from outside the package and the DoD assertions would become inexpressible.
> 2. **`harnessCorpus` is generated, not typed.** One hand-written literal - `"etaoinshrdlcumwfgypbvkjxqz"` plus 26 frequencies. The 676-bigram `NgramsByFrequency()` list is produced by two nested loops and `slices.SortFunc` on descending `freq[a]*freq[b]`, built **once in the constructor** because `ApplyResult` calls `NgramsByFrequency()` on every lesson for its tier clamp and `activeNgrams` walks the prefix on every read. `Transitions(ctx)` ignores its context and returns all 26 keys weighted by unigram frequency - an independence assumption, which makes `TestLessonWords_WalkFollowsGraph` trivially true here (it stays meaningful against `engineFixtures`) but makes the corpus self-consistent, so the top-tier bigrams really are the ones the walk produces.
> 3. **The clock must advance, by the lesson's own duration.** `now` moves forward by the sum of `res.Keys[*].TotalMillis` plus a ~30s gap. Not the ngram millis: a middle character sits in two bigram windows, so summing those double-counts. Without any advance, `LastPracticed == now` forever and `decayedScore` is dead code inside the harness - the decay half of the design would go unexercised by the artifact that exists to exercise it. At the resulting ~35s cadence decay turns out to be nearly inert anyway (500 lessons ≈ 5 hours against `decayTau` = 7 days); a 24h-per-lesson cadence is the opposite failure and would stall progression permanently, since `exp(-1d/7d) = 0.867` drops a key at 0.98 below `unlockKeyThreshold` in two days.
> 4. **A fresh `rand.New(rand.NewPCG(0, 12345))` per learner, not one shared generator.** With a shared `r` the second learner consumes a different stretch of the stream and any difference between the two runs could be the stream rather than the profile. Identically-seeded generators make good-vs-struggling a controlled comparison. **An unobvious consequence, worth knowing:** the number of RNG draws per lesson is independent of the competency state (one `r.IntN` per word for its length, then exactly `targetLen` further draws, then 2 per character in `simulate`), so two runs stay _permanently_ synchronised; and because `updateScore` folds at `alpha = 0.3`, score differences contract by 0.7 per observation and evaporate within a lesson or two. Runs that diverge early re-converge to bit-identical state. This was first noticed as an apparent bug - the struggling learner's output was byte-identical across two `minSamples` values - and is in fact what proved that constant was not its binding gate. It also means single-seed results are less independent than they look; a second seed is the check.
> 5. **Decision 1's `Params`-struct revisit trigger has _not_ fired.** The retunes below were each done by editing the `const` block and re-running, one set at a time. Nothing in step 6 needed to compare two constant sets inside a single run, which is the trigger as written. The block stays.
>
> **Decided during the step - two constants changed, both harness-driven** _(Cory, 2026-08-12)_. These are the first tuning changes made on evidence rather than on the spec's initial guesses, which is what principle 7 said the harness was for.
>
> 1. **`minSamples` 50 → 15.** For the good learner the sample floor, not the score threshold, was the only binding gate: with `TargetWPM` frozen at 40 for the whole alphabet climb (`shouldRaiseTarget` requires _all_ keys unlocked) and a mean interval of 204ms, `speed` in `instant` clamps to `min(1, 300/204) = 1.0`, pinning every key at `0.7×0.98 + 0.3×1.0 = 0.986` against a 0.85 bar. Full alphabet went 604 lessons at 50, 95 at 10, 145 at 15 - and the final per-key scores were unchanged across all three (`z` ends 0.846 / 0.852 / 0.734). ~460 lessons of pure delay bought no measurable competency. 15 still rules out the "lucky high score off three keystrokes" case the floor exists for (step 3's design question). Noted above the `const` block.
> 2. **New `freqExponent = 0.5`, applied as `math.Pow(c.Freq, freqExponent)` in `candidates()` (`generate.go`).** The two factors in `baseFreq * keyFactor * ngramFactor` had incompatible dynamic ranges: corpus frequency spans 127:1 between `e` and `z`, while `keyFactor` is capped at `1 + lambdaKey×1 = 4`. The pedagogical signal was drowned by an order of magnitude, and no value of `lambdaKey` fixes it - equalising would need ≈126, which makes every lesson unreadable the moment any key is weak. The exponent flattens the distribution to 11:1 while preserving the ordering, so lessons stay English-like. It lives in the engine, not the corpus: the real `Provider` will return context-conditional bigram frequencies and this is an engine tuning choice, not a property of the language data. Result: good 145 → **106**; the struggling learner's `z` gap fell from **+3365 to +523** and their full-alphabet lesson from 5841 → **4694**.
>
> Why the struggling learner improved so much less than the tail number suggests: `seeds()` was already frequency-blind (`1 + lambdaKey*need`), and it supplies 15 of the ~60 characters in a lesson. Fixing `candidates()` raises a rare key's share of the _other_ 45 by ~7× but total exposure by only ~40%. The bottleneck redistributed rather than vanished - `k` and `q` now carry gaps of +889 and +1274 where `z` used to carry +3365.
>
> **Findings the harness produced that were _not_ acted on** - recorded deliberately, so the next change is made on evidence rather than blind:
>
> 1. **The target-WPM raise overshoots by ~25 WPM, and ratchets.** Steady-state equilibrium for the good profile is `speed* = (targetRaiseScore - wAccuracy×0.98)/wSpeed = 0.547`, and at 58.8 actual WPM that is a target of ~110 (at 110, the score is 0.846, already under `targetRaiseScore`). Observed: **135**, in every run, unaffected by both constant changes. The raise loop is a feedback controller reading a lagging signal - `ApplyResult` scores against the _pre-raise_ `s.TargetWPM`, and `meanKeyDecayedScore` averages in rare keys that receive well under one observation per lesson and hold stale high scores for dozens of lessons while the target climbs 5 WPM each time. The landing point confirms the model: at 135 the predicted score is 0.817 and the measured mean across 26 keys is 0.821. **The consequence is structural, not cosmetic:** the target never falls, so it parks every score permanently at ~0.82 - below `unlockKeyThreshold` (0.85) and barely above `unlockNgramThreshold` (0.80). A difficulty controller whose setpoint _equals_ the unlock threshold leaves the learner permanently at the edge of every gate, and the tuning table in [`engine.md`](../engine.md) does not show that the three constants are coupled. `targetRaiseScore` probably needs to sit meaningfully above `unlockKeyThreshold`. Expect this to be the cause when the ngram tier stalls.
> 2. **The struggling learner is limited by variance on rare keys, not by either gate directly.** `minSamples` was irrelevant to it - at the moment before `z` unlocks, the 25 already-unlocked keys carry sample counts in the thousands. The real mechanism is per-lesson attempt count: at ~0.4 attempts per lesson, `accuracy` in `instant` can only be 0.0 or 1.0, so a single mistype collapses the instant score to ~0.2 and the EMA needs many subsequent clean observations to recover. `allMastered` is an AND over all 26 keys, so one volatile key stalls everything. Post-`freqExponent` the struggling learner still ends with `m` at 0.610, `f` at 0.625 and `v` at 0.581. **The relaxation of `allMastered` to a mean-plus-floor rule was considered and rejected** - advancing while an item is still weak is a pedagogical claim we are not willing to make. Improving exposure further, or damping the instant score at low attempt counts, are the live alternatives.
> 3. **The ngram tier is alive but barely: it advances once in 200 lessons, and after that never again.** `TestHarness_NgramTierAdvances` reports `20 -> 21`, last advance at lesson 104 of 200, for the good learner. Two distinct mechanisms produce that, and the per-ngram log separates them cleanly. **Exposure is not the constraint:** all 21 active bigrams carry 20-72 samples, every one comfortably over `minSamples`. **The score is.** Fourteen of the 21 sit in a tight band of 0.758-0.770 and the other seven at 0.550-0.720 - so _every_ active ngram is below `unlockNgramThreshold` (0.80), and `shouldAdvanceNgramTier` cannot fire again for the rest of the user's life. The band is the target-overshoot equilibrium from (1) landing on the ngram axis: a bigram's `TotalMillis` covers two keystrokes (`ks[i-1].ms + ks[i].ms` in `simulate`) but is scored against `targetMs`, which is per-character, so at `TargetWPM = 40` a good learner's bigram sits near 0.89 and at 135 it collapses to ~0.74. (Predicted 0.737 against a 408ms two-keystroke interval; measured 0.765, which implies an effective ~290ms. The residual is unexplained and worth a look before anyone leans on the derivation.) The seven stragglers are the second mechanism, the same single-observation variance that limits the struggling learner in (2): one mistyped instance takes a bigram from 0.89 to 0.69 in one EMA step, and post-alphabet an in-scope bigram receives only ~0.2-0.35 observations per lesson - 45 bigram instances per lesson spread across 676 possible pairs, with the in-scope boost capped at `1 + lambdaNgramKeyPhase` = 1.5× - so recovery takes something like ten lessons, not two. A third of the active set is therefore depressed at any moment, which is why the conjunction failed for 104 lessons even _before_ the target overshoot dragged the whole band under the bar. **The 1.5× in-scope boost is the ngram-channel version of the dynamic-range defect `freqExponent` fixed for keys** - 21 in-scope pairs competing against 676 possible ones cannot be rescued by a 1.5× multiplier. Not acted on: Decision 3 commits phase 3 to a thin tier axis, and the DoD asks only that the axis be alive.
> 4. **Locking keys/ngrams back down when performance degrades** - raised here, deferred to the roadmap's future work rather than built, since the engine's existing decay + `need` weighting already achieves the soft form and this is an increment on a working mechanism, not a prerequisite for a deployable prototype. The non-obvious constraint to record before anyone builds it: **it needs hysteresis or it oscillates.** Lock `z` at 0.84 and the remaining 25 keys' mean immediately passes, so `nextKeyToUnlock` re-adds it as `ItemScore{}` at score 0 - strictly worse than the 0.84 removed. Two thresholds are required (lock below X, unlock above Y, Y > X), plus a rule that a re-locked key returns with its prior score. It also breaks the invariant `nextKeyToUnlock` relies on, that the unlocked set is a _prefix_ of `KeyOrder()`, since a hole opens mid-order.
>
> **All five assertions are written and green**, each verified by making the single change it exists to catch and confirming red: good learner bounded (`unlockKeyThreshold` → 0.99); struggling strictly slower than good (`instant` → `return 1.0, true`); the phase flip fires and orders after the alphabet; the target-WPM equilibrium; and `TestHarness_NgramTierAdvances` (`startingNgramTier` → 0, which reproduces the documented dead-on-arrival case exactly). The target-WPM assertion is derived from the constants rather than hard-coded at 135, and has to allow for the overshoot in (1) rather than the analytic equilibrium of 110.

### Step 7 - `InitialCompetency()`, JSON marshaling, and closing the seams

**Write:**

```go
func InitialCompetency(c Corpus) CompetencyState // startingKeys most-frequent keys unlocked at zero score, NgramTier init, TargetWPM=40
```

> **Corrected in step 6: it takes a `Corpus`.** The no-argument signature this plan originally specified cannot work - the starting set is `c.KeyOrder()[:min(startingKeys, len(order))]`, and a hand-written `"etao"` literal duplicates data the corpus owns. The failure when they disagree is quiet: `nextKeyToUnlock` returns the first key in `KeyOrder()` _not_ present in `s.Keys`, so a state seeded from a different order leaves the unlocked set no longer a prefix of `KeyOrder()`, and "unlock the next most frequent key" silently stops being true. Same class of omission as step 4's missing corpus on `ApplyResult`, and for the same reason: state records what is unlocked, only the corpus records what is next.
>
> **Also settled in step 6: `startingNgramTier = 20`.** The TODO below says to seed a non-zero tier; 20 is the value, and the constraint that fixes it is that at least one bigram over the `startingKeys` most-frequent letters must be in scope, or `activeNgrams` is empty, `shouldAdvanceNgramTier` returns false on the empty set, and the axis never starts. Over `e,t,a,o` the first formable English bigram is roughly 8th by frequency and the second roughly 14th, so 20 leaves headroom without pre-unlocking a wide band. Cheap guard for the harness: `len(activeNgrams(InitialCompetency(c), c)) == 0` → `t.Fatal` at lesson 0.
>
> ~~**Open:** `Corpus.StartingKeys()` and the `startingKeys` constant are now two sources for one number~~ - **done in step 6** (`584b43d`): deleted from the interface, since how many keys a beginner starts with is a pedagogy decision belonging beside `unlockKeyThreshold` in the tuning table, not a property of the language data. `corpus.Provider` is three methods, not four.

**TODO:** **seed a non-zero `NgramTier`** - with `NgramTier: 0` there are no active ngrams, `shouldAdvanceNgramTier` returns false on the empty set, and the tier never grows: the ngram axis is dead on arrival. (Found in step 4.) Then add JSON tags to the engine types so `CompetencyState` marshals to the `competency` document in [`../schema.md`](../schema.md) exactly: `keys` / `ngrams` maps of `{score, samples, last_practiced}`, plus `ngram_tier` and `target_wpm`. Then close the two seams: `internal/progress/service.go:26` uses `json.Marshal(engine.InitialCompetency())` instead of `{"version":0}`; resolve `cmd/server/main.go:38` (with package-level funcs there's little to "construct" - mostly it's wiring `corpus.Provider` so the eventual `/lessons/next` in phase 4 has a corpus; keep this minimal and honest about what phase 3 actually needs). Remove the nolint directive in `types.go` if still present.

**The design question:** this is where the pure engine type meets the persisted shape. Coupling the engine struct's JSON tags to the DB document is a deliberate, ADR-0009-blessed choice (the JSONB doc _is_ the `CompetencyState`) - but it means a field rename in the engine is a migration concern. Worth a one-line comment at the type saying so.

**Prove it:** a round-trip test - `InitialCompetency()` → `json.Marshal` → assert it matches the schema-doc example's shape; unmarshal back → equal state. **Checkpoint:** register a new user against the real server → the persisted `competency` row is a valid initial `CompetencyState`, not `{"version":0}`.

> **Decided during the step - `map[rune]` does not marshal to the document the schema doc specifies** _(Cory, 2026-08-12)_. A `rune` is an `int32`, and `encoding/json` writes integer map keys as their quoted decimal, so `Keys` persisted `'e'` as `"101"`. Legal JSON, entirely unlike [`../schema.md`](../schema.md), and silent - `Ngrams` is `map[string]` and was already correct, and nothing downstream would have noticed, because `progress.Handler` decodes the stored bytes into an untyped `openapi.Competency` (`map[string]any`) and passes them straight to the client. `ItemScore` was also still untagged from step 6's tagging commit, so its fields persisted as `Score` / `Samples` / `LastPracticed`.
>
> **The fix is a named map type, `KeyScores`, carrying its own `MarshalJSON` / `UnmarshalJSON`.** Chosen over two alternatives:
>
> 1. **`type Key rune` with a `MarshalText` method** - json's actual documented extension point for map keys, and arguably the better domain model, since `Key` is a real noun in this package. Rejected because avoiding a confusing `rune`/`Key` split means propagating it through `Corpus.KeyOrder() []Key`, `Candidate.Char` and `Result.Keys` - a package-wide vocabulary change, reaching into `internal/corpus`, made for a serialisation reason. Worth revisiting if `Key` ever earns its keep on domain grounds; not on these.
> 2. **`MarshalJSON` on `CompetencyState` itself** - one place for all wire knowledge, but it needs a shadow struct restating every field, so each new field must be added in three places and forgetting one fails silently.
>
> The named map type wins on blast radius: a named map type is assignable to and from the unnamed one in both directions, so every existing `map[rune]ItemScore{...}` literal, `make(...)`, `maps.Clone` result and `keysUnlocked` argument compiled unchanged. **Zero call sites touched**, ~35 new lines in `types.go`.
>
> Two implementation details that are the actual traps:
>
> - **`MarshalJSON` takes a value receiver, not a pointer.** With a pointer receiver the method is absent from `KeyScores`' method set, and `json.Marshal(state)` on a `CompetencyState` _value_ - which is how every caller holds one - falls back to the integer encoding with no error. Verified directly: the same struct marshals `{"keys":{"101":...}}` by value and `{"keys":{"e":...}}` by pointer. Flipping the receiver in our body happens to stop compiling on a `len(ks)`, which is luck rather than protection.
> - **`UnmarshalJSON` rejects any key that is not exactly one rune.** `[]rune(s)[0]` on `"th"` would decode to `'t'` and silently add a key the user never earned to the unlock set - and the unlock set is what `nextKeyToUnlock` reads.
>
> **Decided: the round-trip test is not sufficient on its own** _(Cory, 2026-08-12)_. `marshal_test.go` pairs it with `TestCompetencyState_MarshalsToSchemaDocument`, which compares against the document from [`../schema.md`](../schema.md) restated verbatim in the test. This is not redundancy: reverting `Keys` to `map[rune]ItemScore` leaves the round-trip test **green**, because marshal and unmarshal stay self-consistent about `"101"`. A round trip proves no information is lost; only an external restatement proves the shape is the agreed one. (Both were confirmed red by mutation - `KeysAreCharactersNotCodePoints` and the schema test by reverting the field type, the round trip by hiding `UnmarshalJSON` from json, the single-rune guard by removing it, and the nil-map case by dropping the `make`.) Comparison is on decoded values rather than bytes, since field order is not part of the JSON contract; the compacted text is printed only on failure.
>
> **Noted, not fixed: `time.Time` does not survive a round trip under `==`.** Marshalling strips the monotonic clock reading `time.Now()` attaches, so a persisted-then-loaded state compares unequal to its in-memory original. Harmless here because every fixture timestamp comes from `time.Date`, but it is a live trap for the phase-4 session-submit path, which will hold both. Recorded as a comment in the round-trip test.
>
> **Sequencing: the two seams close after the corpus track, not before it** _(Cory, 2026-08-12)_. `InitialCompetency` takes a `Corpus` (corrected in step 6), and `corpus.Provider` is still an empty struct - so `progress.Initialiser` has nothing real to call and `main.go` has nothing real to pass. The options were a stopgap hand-written corpus on the production wiring path, or reordering. Reordered: the pure half of step 7 (tags, `KeyScores`, `marshal_test.go`) is done now against the fake corpus, then C1 + C2, then both seams and the register-a-user checkpoint land together against the real `Provider`. The seam then gets written once, and no commit ships a fake in `cmd/server`. ~~**Still open at the end of this session:** `internal/progress/service.go:25`, `cmd/server/main.go:38`, and the checkpoint.~~ **All three closed 2026-08-20**, after C1-C3, exactly in the order this note set out. See [the corpus track](#the-corpus-track-build-after-the-engine-swap-behind-the-interface) for what the seams turned out to need.

## The corpus track (build after the engine, swap behind the interface)

Independent of the engine's logic; gated only by the frequency-validation test. Because the engine was built against a fake `Corpus`, this slots in behind the same interface with no engine change - that's the payoff of Decision 2.

- **C1 - `cmd/corpusgen`.** _Done 2026-08-20._ A committed offline generator ([ADR 0013](../adr/0013-corpus-as-embedded-generated-data.md)) over three Project Gutenberg texts committed under `internal/corpus/data/sources/` (Melville, Austen, Homer). `internal/corpus/gen` holds the pure counting - `strip` (Gutenberg boilerplate), `words` (an `iter.Seq[string]` that lowercases and discards any token containing anything outside a-z, matching what the Norvig reference does), and `Count`, the single exported entry point. `main.go` is flags → read → count → `json.MarshalIndent` → write. **No new dependencies**, as the plan predicted.
- **C2 - `corpus.Provider`.** _Done 2026-08-20._ `//go:embed data/corpus.json`, parsed once by `New() (*Provider, error)` - a constructor returning an error rather than an `init()` that panics, so the composition root decides what a bad embed means. Three pre-computed fields, three one-line methods, because `ApplyResult` calls `NgramsByFrequency()` on every lesson and must not re-sort.
- **C3 - frequency-validation test.** _Done 2026-08-20._ `internal/corpus/frequency_test.go`, five tests against the [Norvig/Mayzner](https://norvig.com/mayzner.html) reference restated as a literal with its retrieval date. Thresholds and their measured justifications are in the file; all three were **confirmed load-bearing by mutation** (tightening each by one notch turns it red).

### What the corpus track actually decided _(Cory, 2026-08-20)_

**The artifact stores counts and nothing else.** C1 above originally asked the generator to emit three things: the letter frequency order, the ranked ngram list, and the `context → []Candidate` transition graph. It emits none of them. `corpus.json` is two maps - `letters` and `bigrams`, raw integer counts - and all three derived structures are built by `Provider.New()` at load time. Three consequences, in increasing order of how much they mattered:

1. **The transition graph was never a separate structure.** A bigram _is_ a transition: `"th"` means "from `t`, an `h` may follow". `generate.go` only ever calls `Transitions` with a single-character context (`word()` holds `context` as a `rune` and passes `string(context)`), so grouping the bigram table by first letter is a complete answer for phase 3. The `Corpus.Transitions(context string)` signature stays `string` rather than `rune` because it is shaped for the deferred trigram case, where `candidates()`'s `g := context + string(c.Char)` widens with no change.
2. **Determinism came for free, and the tie-break got better.** The worry recorded in `gen.go` was that equal-count bigrams would leak map iteration order into the artifact and cut a different `startingNgramTier = 20` boundary on different builds. Storing maps of counts sidesteps it entirely: `encoding/json` sorts map keys when marshalling, so regeneration is byte-stable, and the `(count desc, string asc)` tie-break lives in `sortedByCount` where it can be read and changed - rather than being frozen into the data by an accident of iteration order.
3. **`Freq` is normalised per context**, so each candidate list sums to 1.0. Strictly unnecessary today - a per-context constant factor cancels inside `weightedSample`, so raw counts would sample identically - but it keeps the real corpus on the same numeric scale the `freqExponent` / `lambdaKey` constants were tuned against with the harness fake, and costs two lines.

**Zero-count bigrams are omitted.** 499 of the 676 possible pairs occur in these three books. Every one of the 26 letters has at least one successor, so no seed can starve on its first step (pinned by `TestTransitions_NonEmptyForEveryLetter`); `q` has exactly one candidate, `u`.

**The corpus is close to the reference but not equal to it, in an explainable way.** Largest letter deviation is `h`, at 6.70% against the reference's 5.05%. Three works of literary narrative are dense in _the/that/this/he/his/her_, and the bigram side sees the same thing independently - `ha` and `hi` rank in this corpus's top 20 and not in Norvig's. This is why `TestKeyOrder_LeadingRunMatchesReference` compares the first **six** letters and not the "~8" this plan originally guessed: both orders open `{e,t,a,o,i,n}` as a set, and at seven they genuinely diverge (`h` here, `s` there). The plan's original assertion would have failed.

**The seams needed one shape change beyond the plan.** `auth.Service` receives a `func(pgx.Tx) auth.ProgressInitialiser`, so there was nowhere to put a corpus. `newProgressInitialiser` became a factory returning that factory, closing over an `engine.Corpus` - which also keeps `internal/progress` importing only `internal/engine`, never `internal/corpus`. `internal/auth`'s integration test mirrors the same shape, against the real embedded corpus rather than a fake, since it exists to mirror production wiring and `corpus.New()` has no external dependency.

**On `cmd/server/main.go:38` ("construct the engine").** The honest resolution, as the plan's open-questions entry predicted: there is no engine to construct - it is package-level pure functions per Decision 1. The corpus is the only real dependency, built once immediately after `logging.Setup` so a bad embed fails before a database pool is opened.

**Checkpoint met.** `POST /api/v1/auth/register` against the running server persists `{"keys":{"a","e","o","t" at zero},"ngrams":{},"ngram_tier":20,"target_wpm":40}` - character keys, not code points, through a real `jsonb` round trip. `TestE2E_RegisterLoginProgress` now asserts this rather than merely asserting the response is non-empty; it decodes into a locally-declared struct rather than `engine.CompetencyState`, for the same reason `marshal_test.go` restates the schema document instead of round-tripping alone - decoding with the engine's own type would agree with whatever the engine happens to do.

## Testing strategy

- **Unit (pure, no I/O):** `scoring_test.go`, `progression_test.go`, `engine_test.go` (ApplyResult) - table-driven, deterministic via injected `now`.
- **Property-based (`testing/quick`):** the selection invariants in step 5 - "any valid state ⇒ a lesson of only unlocked keys" is the canonical one the roadmap names.
- **Seeded statistical:** "weak keys appear more often than strong" - assert over many runs under a fixed seed, with a margin, not an exact count.
- **Harness (end-to-end, still pure):** the bounded-convergence and phase-flip proofs in step 6 - the DoD.
- **Corpus:** the C3 frequency-validation test.

No DB and no HTTP in this package's tests - if a test needs either, the logic has leaked out of the engine and should move to the service layer (phase 4).

## Documentation & tooling impact

- **`go.mod`:** **held - zero new dependencies** across the whole phase (`git diff` on `go.mod`/`go.sum` is empty). The corpus track added `embed`, `iter`, `maps`, `slices` and `cmp`, all stdlib. Originally: expect **no new dependencies** - `math`, `math/rand`, `time`, `encoding/json`, `embed` are all stdlib. (A nice contrast with phase 2, which pulled in argon2/jwt; worth noting for the portfolio.) If a corpus source needs a parser, prefer stdlib first and justify anything more.
- **ADRs:** expect **none new.** The cross-cutting engine decisions are already recorded - [0012](../adr/0012-targets-set-by-tool-not-user.md) (targets by tool), [0013](../adr/0013-corpus-as-embedded-generated-data.md) (corpus embedded/generated), [0014](../adr/0014-engine-as-library-state-follows-identity.md) (engine as library), [0009](../adr/0009-json-serialised-data.md) (JSON competency doc). The engine's internal math is documented by [`engine.md`](../engine.md) (a design doc, the right vehicle) plus code comments. The constants-block choice, the `Candidate` placement, and bigrams-only all fail the ADR reach/opacity bars → package or code comments, not new numbers. This keeps the ADR index from sprawling, which is itself the correct call to be able to defend.
- **`internal/corpus/data/`:** _done._ Committed generated artifact (`corpus.json`, two count maps) plus `data/sources/` - the three Project Gutenberg texts it is derived from, committed so the generation is reproducible from the repo alone, which is what makes ADR 0013's provenance claim checkable rather than merely stated.
- **Makefile:** _done_ - `make corpus` regenerates the artifact from the committed sources. Named for the output, not the command. **Not done: a test that regenerates and asserts `corpus.json` is unchanged**, which `gen.go` floated as a way to enforce byte-stability. Deferred rather than dropped: `encoding/json` sorts map keys, so the output is already deterministic by construction, and the test would need the generator's file I/O inside a package that is otherwise pure. Worth adding if the generator ever grows a step whose output order is not obviously fixed.

## Open questions / seams for later

- **Trigrams** - deferred; the generator may emit them, the engine ignores them in phase 3 (Decision 3).
- **Velocity / anti-plateau generation** - explicitly deferred by the design doc (an online scalar, not a history table); sequence it after the harness exists.
- **Real-word dictionary** as a late-game variety source - out of v1 per [ADR 0013](../adr/0013-corpus-as-embedded-generated-data.md).
- **Key-introduction order** - frequency for now; pedagogical (home-row-first) is an open question.
- **Constants → `Params` struct** - promote only when the harness needs to sweep multiple sets in one run (Decision 1's trigger).
- **`cmd/server/main.go:38`** - phase 3 wires just enough corpus for the engine to be constructible; the real consumer (`GET /lessons/next`, the transactional `POST /sessions`) is phase 4.
- **The ngram-advance quantifier** - `allMastered` is an AND over every active ngram, with decay. At tier 5 that's fine; at tier 150 it's an AND over 150 decaying items and it will effectively never pass. Ideas: make decay very slow, or gate on a window (the most recently added band) rather than everything in scope. Note this is a property of the _quantifier_, not of the tier representation - a stored unlock set would have it identically. The step-6 harness is what will show whether it bites. **_Measured in step 6: it bites, and at tier 20 rather than tier 150._** The good learner advances the tier exactly once in 200 lessons (`20 -> 21`, at lesson 104), with every active bigram well over `minSamples` - so the AND, not exposure, is what fails. The estimate in this bullet was an order of magnitude optimistic because it assumed the failure needs _many_ items; in fact 21 items suffice, since each one's score collapses from 0.89 to 0.69 on a single mistyped instance and recovers only over ~10 lessons at ~0.2-0.35 observations per lesson. "Gate on a window rather than everything in scope" is accordingly the stronger of the two ideas listed here - slowing decay does nothing, because decay is not what depresses the stragglers. The same quantifier also bites on the **key** axis, stalling the struggling learner via an AND over 26 keys where one rare high-variance key holds up everything. See step 6's findings for the full measurement.
- **The in-scope ngram boost has the dynamic-range defect `freqExponent` fixed for keys** _(found in step 6)_ - `candidates()` multiplies an in-scope bigram by at most `1 + lambdaNgramKeyPhase` = 1.5× (3.0 in the ngram phase), while the in-scope set is 21 pairs out of 676 possible. Measured consequence: an in-scope bigram receives ~0.2-0.35 observations per lesson once the alphabet is open, so a single mistyped instance takes ~10 lessons to recover from, and roughly a third of the active set is below its own equilibrium at any moment. Same shape as the key-channel defect, same class of fix; deliberately not applied in phase 3 under Decision 3.
- **Target raise vs. a fresh unlock** _(found in step 4)_ - a newly unlocked key enters at score 0 and is immediately included in `meanKeyDecayedScore`, dragging the mean down. So the target raise structurally cannot fire in the same `ApplyResult` call that unlocks the final key unless the alphabet is large enough to absorb the zero: 8 mastered keys at 0.86 plus one zero averages 0.76 and fails, whereas 25 at 0.90 plus one zero averages 0.865 and passes. Harmless at 26 keys, but it means the interaction is size-dependent - worth watching in the harness rather than fixing blind.
- **`targetRaiseScore` is coupled to the unlock thresholds** _(found in step 6)_ - the raise loop overshoots its equilibrium and never falls back, parking every score at ~0.82: below `unlockKeyThreshold` (0.85), barely above `unlockNgramThreshold` (0.80). Full derivation and measurements in step 6's findings. **Confirmed on the ngram axis:** after the target settles at 135, all 21 active bigrams sit at 0.55-0.77, every one under `unlockNgramThreshold` (0.80), and the tier cannot advance again. Decide whether `targetRaiseScore` should sit above both unlock thresholds, and whether the target should be allowed to fall as well as rise, before any attempt to tune the ngram tier - the tier stalls on this, so tier tuning done first would be tuning against a moving floor.
- **`Targets` has no phase weighting** _(found in step 5)_ - the ranking is a single sort on need, so a key and an ngram at equal need tie, and the enumeration order breaks it in the key's favour. During `PhaseNgrams` you would arguably want the reverse. Harmless for now because nothing consumes `Targets` until the phase-4 client renders it, but decide before it becomes a visible behaviour.
- **A zero `CompetencyState` bootstraps itself** _(found in step 4)_ - `allMastered` is vacuously true over an empty key set, so `ApplyResult` on a zero state unlocks one key per call. Harmless in practice (real states come from `InitialCompetency`), but it is emergent rather than designed, and the current zero-state test only asserts no panic - it does not pin the unlock. Decide whether the behaviour is wanted and assert it either way. Revisit if a caller ever legitimately holds an empty state.
- Space as a scored item, where it can be evaluated on its merits instead of arriving as a side effect of a tokeniser rule.
