# Engine Design

> Design doc for `internal/engine`. Covers the competency model, scoring, progression, and lesson generation. This is the spec the package implements and the tests assert against.

## Summary

The engine takes a snapshot of a user's typing competency plus a corpus of language data, and returns the next lesson - a short block of pronounceable, english-like words biased toward the user's current weak points. After a lesson is completed, the engine folds the result back into competency state and decides whether to introduce new content.

It is pure: `state in → state out`, no I/O, all randomness injected. The `progress` and `session` services translate between persisted models and the engine's local types and call into it; the engine never calls out.

## What we borrow from keybr, and what we add

[keybr's model](https://keybr.com/help), distilled: track per-key speed and accuracy; give each key a confidence that grows with sample count; start from a small set of the most frequent letters; introduce a new letter once the current set clears a speed/accuracy threshold; bias generation toward weak keys so mastered keys fade and problem keys recur; let the user set their own target speed as the threshold. Lessons are phonetic pseudo-words, so generation already _uses_ ngram structure (the letter combinations that occur in the language) to avoid nonsense like `zx`.

The gap, and my contribution: keybr uses ngram structure to _generate_ but never _scores or progresses on ngrams as first-class items_. I track, score, and gate on ngrams the same way keybr does for single keys. The same ngram model that drives generation also defines a second competency dimension and a second progression axis. **The ngram model is simultaneously the lesson generator and a scored competency dimension.**

## Core model

### Items

An _item_ is the unit of competency. There are two kinds:

- **Keys** - single runes (`e`, `t`, `;`, ...).
- **Ngrams** - short character sequences that occur in the language (`th`,
  `ing`, `he`, ...). v1 uses bigrams and trigrams.

Keys and ngrams are scored with the same machinery. The only differences are how they are unlocked (below) and that an ngram is only ever _active_ if all of its constituent keys are unlocked.

### ItemScore

Each item carries a smoothed competency estimate plus enough metadata to compute confidence and recency-decay:

```go
type ItemScore struct {
    Score         float64   // smoothed competency, [0,1]; higher = better
    Samples       int       // keystrokes observed for this item; confidence
    LastPracticed time.Time // for recency decay
}
```

`Score` is what selection and unlocking read. `Samples` expresses confidence: a high score off three keystrokes is not trustworthy. `LastPracticed` lets us decay an item's _effective_ score at read time so neglected items resurface.

### CompetencyState

```go
type CompetencyState struct {
    Keys      map[rune]ItemScore
    Ngrams    map[string]ItemScore
    NgramTier int  // how many of the frequency-ranked ngrams are in scope
    TargetWPM int  // tool-managed; starts at STARTING_TARGET_WPM (40), raised as the user improves (ADR 0012)
}
```

Presence of a key in `Keys` means it is unlocked. Same for `Ngrams`. This is the JSONB document persisted per user (see the schema ADR); it maps 1:1 to the engine's working type, which is the point.

### Result (input to ApplyResult)

The client aggregates per-item stats during a lesson and submits a summary - we do **not** ship or store raw keystrokes (see [Client observation model](#client-observation-model) below, and the [schema doc](schema.md)).

```go
type Observation struct {
    Attempts    int     // times this item was typed in the lesson
    Errors      int     // of those, how many were wrong
    TotalMillis float64 // cumulative time across attempts
}

type Result struct {
    Keys   map[rune]Observation
    Ngrams map[string]Observation
}
```

### Lesson (output of NextLesson)

```go
type Lesson struct {
    Words   []string // 10-15 generated words
    Targets []string // items this lesson was built to exercise (telemetry)
}
```

## Client observation model

The engine consumes `Observation`s but never produces them: the client measures them while the user types, then submits the per-key and per-ngram summaries. (On the identified path these go to `POST /sessions`; on the offline path they feed straight into `ApplyResult` in-process - see [ADR 0014](adr/0014-engine-as-library-state-follows-identity.md).)
To make that measurement unambiguous, v1 fixes both the input model and the attribution rules here, so every client attributes identically.

**Force-correction input.** The cursor advances only when the correct key is pressed; a wrong key is rejected and counts as an error at that position. This matches keybr, aligns with the accuracy-first scoring weights, and - crucially - removes the ambiguity that free typing (backspaces, skipped errors, drift between typed and intended text) would otherwise inject into attribution. The typed text and the intended text stay aligned by construction.

**Attribution.** For a lesson whose intended text is a known character sequence:

- _Key_ `k` at position `i`: `Attempts += 1`; `Errors += 1` if the **first** keystroke at `i` was wrong (regardless of how many wrong keys followed before the correct one); `TotalMillis +=` the interval between the correct keystroke at `i-1` and the correct keystroke at `i`.
- _Ngram_ `g`, for each length-`n` window of the intended text whose characters are all active items: `Attempts += 1`; `Errors += 1` if any character in the window had a first-try error; `TotalMillis +=` the summed per-character intervals across the window.
- Word-boundary spaces are not scored as keys in v1, and ngrams do not span them.

Because nothing keystroke-level is transmitted or stored - only the aggregated `Observation`s - this is consistent with the no-raw-keystroke decision in the [schema doc](schema.md). Keeping the rules here rather than in the client means the standalone TUI, the SSH TUI, and any future client (e.g. a web client re-implementing them) are held to one spec.

## Scoring

For a single item's observation in a completed lesson:

```
accuracy   = (Attempts - Errors) / Attempts                  // [0,1]
meanKeyMs  = TotalMillis / Attempts
targetMs   = 60000 / (TargetWPM * 5)                          // ms per char at target
speed      = clamp(targetMs / meanKeyMs, 0, 1)                // 1.0 == at/above target
instant    = W_ACCURACY*accuracy + W_SPEED*speed             // [0,1]
```

`W_ACCURACY = 0.7`, `W_SPEED = 0.3`: accuracy dominates, because for _learning_ you want correctness first and speed second. (The 5-chars-per-word convention is the standard WPM definition; it lives in the constants table as `charsPerWord` because a different corpus or language could justify changing it, unlike the millisecond conversion beside it, which is arithmetic.)

**Invalid observations are rejected, not scored.** `Attempts == 0`, `TotalMillis == 0`, or `TargetWPM == 0` are all caller bugs rather than runtime conditions, but none of them has a meaningful score: the first two make the formulas undefined, and the third would divide by zero. So `instant` and `updateScore` return a `(value, ok bool)` pair and report `ok == false` for these inputs, rather than returning a bare `0` that is indistinguishable from a legitimately terrible score and would fold silently into stored state. On `!ok`, `updateScore` returns `prev` unchanged, so a caller that ignores the flag no-ops instead of erasing the item's history. Nothing is logged - the engine performs no I/O (it runs in-process inside the TUI, where a default `slog` handler would write over the render), so surfacing a malformed submission to a user is the calling service's job, at the boundary where the `Result` is unmarshalled.

The stored score is an exponential moving average of `instant`, which gives keybr's "confidence based on recent performance" behaviour - recent runs matter more, but one bad run doesn't erase history:

```
Score'   = ALPHA * instant + (1 - ALPHA) * Score   // ALPHA = 0.3
Samples' = Samples + Attempts
```

For a brand-new item (`Samples == 0`), `Score' = instant`.

### Recency decay

Items not practiced recently should drift down so the engine revisits them. We do this at **read time** as a pure function of timestamps - no background job, fully deterministic, trivially testable:

```
age          = now - LastPracticed
decayedScore = Score * exp(-age / TAU)             // TAU = 7 days
```

Selection and unlocking use `decayedScore`, never the raw `Score`. The stored history stays honest; decay is a lens applied when reading.

## Progression

Two axes. Keys are the breadth of the alphabet; ngram tier is the depth of combinations practiced. They interact: an ngram is active only once all its letters are unlocked.

### Key unlocking

- Start from `corpus.StartingKeys()` - the N most frequent letters (keybr uses `e n i t r l`); `N` is a constant, default 4.
- New keys are introduced one at a time, in `corpus.KeyOrder()` (frequency order), when **every** currently-unlocked key clears the bar:

```
unlock next key  ⟺  for all unlocked k:
                       decayedScore(k) >= UNLOCK_KEY_THRESHOLD   (0.85)
                       and Samples(k)  >= MIN_SAMPLES            (50)
```

The `MIN_SAMPLES` gate prevents a lucky high score off too little data from unlocking prematurely.

### Ngram tiers

Ngrams are ranked once by language frequency in the corpus. `NgramTier` says how many of that ranked list are in scope. An ngram is _active_ if it is within the current tier **and** all its keys are unlocked.

```
advance ngram tier  ⟺  for all active ngrams g:
                          decayedScore(g) >= UNLOCK_NGRAM_THRESHOLD  (0.80)
                          and Samples(g)  >= MIN_SAMPLES
```

### Target-WPM progression

`TargetWPM` is the speed bar every item is scored against (`targetMs` in [Scoring](#scoring)). The tool owns it ([ADR 0012](adr/0012-targets-set-by-tool-not-user.md)); the user never sets it.

- A new user starts at `STARTING_TARGET_WPM` (40), held fixed while the alphabet fills in.
- Once **all** keys are unlocked **and** the mean key `decayedScore` clears `TARGET_RAISE_SCORE` (0.85), raise the target by `TARGET_WPM_STEP` (5):

```
raise target  ⟺  all keys unlocked
                  and mean over unlocked k of decayedScore(k) >= TARGET_RAISE_SCORE
```

Because `instant` is accuracy-weighted, clearing the gate implies accurate, at-speed typing, so the raise is mostly accuracy-driven with speed contributing. At most one raise per `ApplyResult`. There is no hard cap: the target rises only while the user keeps clearing it and holds when they stop - a constant small challenge.

### Phase A → Phase B

To keep the early game simple and to make the "key-focus → ngram-focus" transition in the test sketch concrete, derive a phase:

```
PhaseKeys   = not all keys unlocked, OR mean key decayedScore < PHASE_THRESHOLD (0.75)
PhaseNgrams = otherwise
```

In `PhaseKeys`, ngram weakness contributes little to generation (`LAMBDA_NGRAM` is held low); the lesson is driven by key weakness while the alphabet fills in. In `PhaseNgrams`, `LAMBDA_NGRAM` ramps up and ngram weakness drives generation. This is a soft transition implemented purely through the generation weights below - there is no hard mode switch, which keeps it testable and avoids a jarring change for the user.

## Lesson generation

Generation is a weighted random walk over the corpus's ngram transition graph, restricted to unlocked keys, with transition weights modulated by weakness. It always produces output even when only a handful of keys are unlocked (the early-game problem that a real-word dictionary filter suffers from), and it produces pronounceable english-like words because the transitions come from real language frequencies.

For each step, given the current context (the previous `n-1` characters):

```
candidates = corpus.Transitions(context)          // next chars + base frequency,
                                                  // already restricted to the language
for each candidate c forming ngram g = context+c:
    if any key in g is not unlocked: skip
    w(c) = baseFreq(g)
         * (1 + LAMBDA_KEY   * need(c))           // boost weak keys
         * (1 + LAMBDA_NGRAM * need(g))           // boost weak ngrams, and only
                                                  // when g is active (see below)
sample next char by w(c) using the injected rand
```

where `need(item) = 1 - decayedScore(item)` for known items, and `need = 1.0` for not-yet-practiced active items (so newly unlocked content surfaces hard, matching keybr's behaviour of front-loading a new letter). Insert word boundaries to hit a target word-length distribution; continue until `lessonWords` words are produced. Record the highest-`need` items in `Lesson.Targets`.

`LAMBDA_KEY ≈ 3`, `LAMBDA_NGRAM ≈ 0.5` in `PhaseKeys` ramping to `≈ 3` in `PhaseNgrams`. Because `phaseIsNgrams` is a boolean rather than a ramp, the implementation spells the two ends as separate constants (`lambdaNgramKeyPhase`, `lambdaNgramNgramPhase`) and selects between them once per lesson.

**The ngram factor applies only to _active_ ngrams.** The "active" qualifier on `need` above is load-bearing, and the pseudocode's unconditional multiply is not safe to read literally. `need` is `1 - decayedScore`, and an ngram outside the current tier has no stored score, so it evaluates to the maximum `1.0` - permanently, because `ApplyResult` discards observations for out-of-scope ngrams and nothing can ever lower it. Ungated, every untracked bigram would outrank a mastered in-scope one forever, and `NgramTier` would stop meaning anything for generation. So the factor is `1 + LAMBDA_NGRAM * need(g)` when `g` is in scope and exactly `1.0` when it is not, leaving out-of-scope candidates weighted by frequency and key-need alone.

**A word start has no ngram factor at all.** The first character of a word has no preceding context, so no `g` exists to score. This is worth stating because `need` dispatches on length: passing a bare key through it as a one-rune string silently returns the _key's_ need a second time, double-counting it. The implementation keeps the two paths in separate functions so the mistake is unreachable rather than merely avoided.

> [!INFO] Generated pseudo-words vs. real-word dictionary
> Use the generator as the primary source because it never starves with a small alphabet and because it unifies generation with the ngram model. A real-word dictionary filtered to unlocked letters is a possible later addition for late-game variety.

## The two engine functions

```go
// NextLesson reads current state and produces the next lesson. Pure: all
// randomness flows through r; decay is computed from now.
func NextLesson(s CompetencyState, c Corpus, now time.Time, r *rand.Rand) Lesson

// ApplyResult folds a completed lesson's result into competency state and
// applies any unlocks/tier advances. Pure: now is passed in, no clock read.
func ApplyResult(s CompetencyState, c Corpus, res Result, now time.Time) CompetencyState
```

Both entry points take `c Corpus`. This is easy to miss for `ApplyResult` - folding a result into state reads as self-contained, and the scoring half genuinely is - but the progression half is not: unlocking asks _which key comes next_ and _how deep does the ngram list go_, and only the corpus knows. The parameter is visible rather than hidden in a struct field because the engine's functions are package-level and pure.

`ApplyResult` order of operations: update each observed item's `Score`, `Samples`, `LastPracticed`; then evaluate the key-unlock condition (unlock at most one key per call); then evaluate the ngram-tier condition; then evaluate the target-WPM raise (at most one step per call). Unlocking after scoring means a lesson's own result can trigger the unlock it earned.

### The Corpus dependency

The engine takes `Corpus` as a parameter so the dependency points downward (`internal/corpus` owns the data; `engine` consumes an interface):

```go
type Corpus interface {
    StartingKeys() int
    KeyOrder() []rune                  // frequency order for unlocking
    NgramsByFrequency() []string       // frequency-ranked; defines tiers
    Transitions(context string) []Candidate  // for the generator
}
```

## Tunable constants

Keep these in one block so they are easy to find, tune, and explain. The
`Constant` column is the Go identifier as declared in `internal/engine/engine.go`.

| Constant                | Default | Meaning                                               |
| ----------------------- | ------- | ----------------------------------------------------- |
| `wAccuracy`             | 0.7     | weight of accuracy in instant score                   |
| `wSpeed`                | 0.3     | weight of speed in instant score                      |
| `alpha`                 | 0.3     | EMA smoothing; higher = more reactive                 |
| `decayTau`              | 7 days  | recency decay time constant                           |
| `unlockKeyThreshold`    | 0.85    | min decayed score on all keys to unlock next          |
| `unlockNgramThreshold`  | 0.80    | min decayed score on active ngrams to advance         |
| `minSamples`            | 50      | confidence gate before any unlock                     |
| `phaseThreshold`        | 0.75    | mean key score to enter ngram-focus phase             |
| `startingKeys`          | 4       | size of the initial unlocked set                      |
| `startingTargetWPM`     | 40      | initial target speed; held until the alphabet unlocks |
| `targetRaiseScore`      | 0.85    | mean key decayed score needed to raise the target     |
| `targetWPMStep`         | 5       | WPM added per target raise                            |
| `lambdaKey`             | 3.0     | weak-key boost in generation                          |
| `lambdaNgramKeyPhase`   | 0.5     | weak-ngram boost while the lesson is key-focused      |
| `lambdaNgramNgramPhase` | 3.0     | weak-ngram boost once `phaseIsNgrams`                 |
| `lessonTargets`         | 5       | high-need items a lesson records for telemetry        |
| `lessonWords`           | 15      | words per generated lesson                            |
| `charsPerWord`          | 5       | assumed chars. per avg. word, used to calc. WPM       |
| `minWordLen`            | 2       | shortest generated word                               |
| `maxWordLen`            | 6       | longest generated word                                |

These are guesses, not gospel. Tune them against simulated users (below).

> **A note on notation.** Throughout this document the constants are written in
> the usual mathematical convention - `W_ACCURACY`, `TAU`, `LAMBDA_KEY` - because
> that is how they read as equations. The Go identifiers are the unexported
> lowerCamel equivalents listed in the table above: `wAccuracy`, `decayTau`,
> `lambdaKey`. Go has no SCREAMING_SNAKE convention, and these are engine
> internals rather than part of the package's contract, so they are unexported.
> `TAU` is spelled `decayTau` in code to keep the link to the `exp(-age / TAU)`
> formula explicit at the call site.

## Testability

Everything above is pure, so the tests in the `engine_test.go` sketch fall out directly:

- **Selection invariants** - generate a lesson, assert every character is an unlocked key; assert weak items (low `decayedScore`) appear at higher frequency than strong items across many seeded runs; assert a newly unlocked item appears in the next lesson's `Targets`.
- **Scoring invariants** - feed a perfect-but-slow observation and a fast-but-error-laden one; assert the former scores higher (accuracy weight dominates); feed two identical items with different `LastPracticed`; assert the stale one has a lower `decayedScore`.
- **End-to-end progression** - drive `ApplyResult` in a loop with a simulated user that types at a fixed accuracy/speed; assert all 26 keys unlock within a bounded number of lessons; assert the phase flips from key-focus to ngram-focus once mean key score crosses `PHASE_THRESHOLD`.

Because randomness is injected, seed the `rand.Rand` for deterministic frequency assertions. Property-based tests (`testing/quick`) fit the invariants well: for any valid state, a generated lesson contains only unlocked keys.

The simulated-user harness doubles as a tuning tool: run a few thousand virtual lessons against different constant sets and watch how many lessons it takes a "good" and a "struggling" learner to clear the alphabet. That harness is also a great thing to show an interviewer - it demonstrates you validated the design, not just shipped it.

## Open questions (decide later, note in README/ADR)

- Bigrams only, or bigrams + trigrams? Start with bigrams; add trigrams once the bigram path works end to end. The generator emits both; the engine chooses.
- Real-word dictionary as a late-game variety source layered on top of the generator ([ADR 0013](adr/0013-corpus-as-embedded-generated-data.md) keeps this out of v1).
- Key-introduction order: pure frequency vs. a pedagogical order (home row first). Frequency is the keybr-faithful default.
- **Velocity-aware / anti-plateau generation.** Today every per-item signal is a _level_ (`Score`, `Samples`, `LastPracticed`). A _rate_ signal would let generation react when a user has stopped improving on an item: soft-deweight the stuck item so it stops dominating the lesson, let the user get wins elsewhere, and let the existing recency decay (`TAU`) resurface it. Notes for whoever picks this up:
  - **Don't store a per-item time series to get this.** The rate is a derivative and can be maintained online: a second slower EMA per item (`trend = fastEMA - slowEMA`), or a `Velocity`/`ScorePrev` scalar in `ItemScore`. One or two extra floats, stays inside the competency JSONB, stays a pure O(1) `ApplyResult` update, no new table. If a real least-squares slope is wanted over a proxy, a **bounded** ring buffer (e.g. last 8 scores) in the JSON is the middle option - still in the doc, still bounded, no row explosion. An unbounded per-item history in the JSONB doc is the same load-whole/write-whole anti-pattern we rejected for `sessions` (see [schema doc](schema.md)).
  - Because `Score` blends accuracy and speed (0.7/0.3), a clean _speed_ trajectory needs the speed component tracked separately from accuracy - otherwise "not improving" fires on an accuracy plateau while speed is still climbing.
  - Make it a **soft de-weight, not a hard abandon**: plateaus are normal in motor learning and often precede a breakthrough, so bailing eagerly can starve the item that most needs reps. Gate it with a confidence guard (min samples _and_ min elapsed time, mirroring `MIN_SAMPLES` on unlocks) or it thrashes on noise.
  - **Sequence it after** the base engine and the simulated-user harness exist; the harness is how you'd prove plateau-switching lowers lessons-to-mastery rather than just adding thrash.
  - A per-item **history table** only earns its place when a consumer appears that a scalar genuinely can't feed - user-facing per-item improvement charts, offline re-tuning against real trajectories, or cross-user analytics. That is the same normalised `user_item_scores` table the [schema doc](schema.md) already names as the deferred trigger; keep it deferred until then.
