# Minimal path to a demo

> The shortest honest route from where the code is today to thirty seconds of video showing this thing work. It reorders [`../roadmap.md`](../roadmap.md) rather than replacing it: nothing here is deleted from the roadmap, it is moved behind the demo. Written 2026-08-22, after phase 4's step 3.
>
> **Revised 2026-09-04, before chunk 2 was started.** Chunk 1 is done. Two things the original cut deferred - force-correction input and ngram attribution - are pulled back in, because both turned out to be cheaper inside the typing loop than beside it. The decisions and the build order are in [Chunk 2](#chunk-2---one-tui-screen).

## Why this exists

Two costs had quietly merged, and separating them is most of the fix.

**The depth standard was being applied uniformly.** [`../../AGENTS.md`](../../AGENTS.md)'s rule - the author must be able to defend every line - exists to stop an assistant writing the code. It was never meant to make every line _equally_ deep. An interviewer will probe the engine, the transactional write and the API contract; nobody has ever asked a candidate to defend a repository method's error wrapping. Paying the maximum price on all of it is what made this slow.

**The payoff is structurally too far away.** The engine - the hardest and most interesting part - was built with no way to see it run. Phase 4 is more of the same, and the first moment a human touches this is phase 5. That is demotivating by construction, and it works against the roadmap's own guiding principle: _stay vertical at every step; get a thin request flowing before deepening any one layer._ Finishing all of phase 4 before starting the TUI deepens the server while the vertical stays open.

The risk to this project is not quality. It is not shipping. What already exists - the engine and its simulated-learner harness, twenty-four ADRs, a spec-driven API, auth, property tests - is an unusually strong portfolio piece. What it does not have is a reviewer being able to watch it work.

## The rule this plan is written under

**This document is deliberately thinner than the phase 3 and phase 4 plans, and that is the point.** Phase 4's plan was detailed enough to follow without thinking, which turned building into transcription and stopped it being learning (see its [revision note](phase-4-sessions.md#revision---phase-4-was-cut-down-2026-08-22)). So this one states what each chunk is and what _done_ means, and stops there. Design decisions inside a chunk get made at the time, in front of the code, by the person writing it.

## The depth budget

Spend the justification standard where the questions actually go.

| Defend at depth (ten minutes, from first principles)             | Understand and move on                    |
| ---------------------------------------------------------------- | ----------------------------------------- |
| `internal/engine` - scoring, decay, unlocking, generation         | Repository methods and their error wrapping |
| `POST /sessions` - the transaction, the lock, the ordering        | Handler-to-generated-type mapping          |
| The spec-driven contract and what codegen does and does not enforce | TUI rendering and layout                 |
| Auth: argon2id, JWT validation, default-deny routing              | Test fixtures and helpers                  |
| The bounded-context seams and why the dependencies point down     | Config plumbing                            |

The right-hand column still gets written by hand and still gets understood. It does not get an essay in the commit message.

## Chunk 1 - Finish `POST /sessions`

The last genuinely hard backend piece, and the one the whole vertical is missing.

Already specified and unchanged: [phase 4](phase-4-sessions.md) steps 4 and 5, with Decisions 2 (server-derived WPM/accuracy), 3 (validation), 4 (the tx-bound `CompetencyStore` seam), 6 (`NUMERIC` conversion) and 8 (response shape). Use them - that reasoning is done and it is good.

**Done when:** register → `GET /lessons/next` → submit a fabricated `Result` → `GET /progress` shows competency moved. One iteration, as an e2e test.

**Not in this chunk:** the concurrency test (phase 4 step 7) and `GET /sessions`. Both are deferred below with their triggers.

## Chunk 2 - One TUI screen

`cmd/tui`, Bubble Tea, and the smallest thing that closes the loop for a human.

In scope:

- Token from a `TYPIST_TOKEN` environment variable, base URL from `TYPIST_API_URL` (default `http://localhost:8080`).
- `GET /lessons/next`, render the words.
- Capture keystrokes under force-correction; accumulate per-key and per-bigram `{attempts, errors, total_millis}`.
- `POST /sessions`; show the returned WPM and accuracy.

**Done when:** `go run ./cmd/tui` against a locally running server plays one lesson end to end and the score moves.

Explicitly out, all of it phase 5 proper: the keyboard heatmap, a polished results screen, a login screen, `$XDG_STATE_HOME` token storage, the anonymous/offline in-process engine.

### Decisions, 2026-09-04

Made in front of the code, before writing it, as this plan intends.

- **The API client lives in `cmd/tui`, not `internal/client`.** The only prospective second consumer is `cmd/sshd`, whose survival is an open question below. Extract on the second real occurrence, per `AGENTS.md`.
- **The client unmarshals into the generated `internal/openapi` models.** A spec change then breaks `cmd/tui` at compile time, which is the same argument ADR 0021 makes server-side, and it puts the client behind the drift test. The one seam to write by hand is `map[string]Observation` on the wire against `map[rune]Observation` in the engine - the mirror of `session.toEngineResult`.
- **Bubble Tea only; no lipgloss, no bubbles.** One line of words and one line of score is `fmt.Sprintf` work. No ADR: ADR 0007 and [`../architecture.md`](../architecture.md) already chose Bubble Tea.
- **Force-correction is in, not deferred.** See below.

### Force-correction moves into chunk 2

This plan originally cut force-correction to phase 5 and accepted approximate observations as the price. That trade was wrong, and re-testing it is what this section records.

Free typing has to solve a problem force-correction does not have: typed text drifting out of alignment with intended text. In the Bubble Tea `Update` loop, the spec's rule is the *smaller* branch - on a mismatch, flag the position and return without advancing the cursor; on a match, fold the position and advance. What phase 5 still owes afterwards is the *rendering* of the error state, not the input model.

Two consequences, both good:

- **The observations are spec-exact**, so [`../engine.md`](../engine.md)'s attribution rules hold and the README needs no caveat about approximate numbers.
- **The session history is real enough to tune against**, which is the precondition the deferred engine-tuning work was waiting on.

### Ngrams are in too

Also originally cut, on the assumption they were separable work. They are not, once force-correction is in: force-correction already requires per-position state (`firstTryError`, and the interval since the last correct keystroke), so the accumulator holds a per-position record for the lesson and derives *both* maps at submit time rather than folding into a key map as it types. That shape is the only real decision, and it is one that would otherwise have to be retrofitted.

Given the per-position slice, the ngram pass is one loop per word over length-2 windows: `Attempts += 1`, `Errors += 1` if either position had a first-try error, `TotalMillis +=` both intervals. Three things make it smaller than it looks:

- **Bigrams only.** `internal/corpus/data/corpus.json` carries `letters` and `bigrams` and no trigrams, so a general length-`n` loop would be speculative generality.
- **Iterating per word** gets "ngrams do not span word boundaries" for free.
- **"Whose characters are all active items" is not the client's problem.** `engine.ApplyResult` discards out-of-scope ngrams, and the `SessionSubmission` schema states that submitting cannot expand a user's own unlock set. Submit every in-word window; the engine filters.

Volume is ~26 keys plus at most 75 bigrams, against `session.maxSubmittedItems` of 1000.

### The three attribution edge cases

Settled deliberately, because each one produces either a 400 or a wrong number:

- **Position 0 has no predecessor** to measure an interval from. Start the clock at first render, so position 0's interval includes think time. Slightly inflated, never invalid - whereas skipping it yields `TotalMillis == 0` for a rune that occurs once in the lesson, which `session.validateObservation` rejects, failing the whole submission.
- **Spaces are typed but not scored.** The typist presses space between words and the cursor advances; no observation is recorded for `' '`. The next key's interval runs from the space keystroke, which is honest - that pause is real typing time.
- **Quitting early submits only the positions reached**, and submits nothing at all from position 0, because `session.validate` rejects an empty `Keys` map.

One defensive detail: clamp each position's interval to a minimum of 1ms. Two keystrokes inside the same millisecond truncate to zero, and a zero is a 400.

### Build order

Four steps, each one runnable, so the loop is visible before it is complete.

1. **The client, no TUI.** `cmd/tui/client.go` plus a `main.go` that reads the two environment variables, calls `GET /lessons/next`, and prints the words. Decode a non-2xx into `openapi.Problem` and print its `title` and `detail`; every later failure is then legible instead of `unexpected status 401`. *Visible:* fifteen words printed.
2. **The Bubble Tea skeleton.** One `tea.Model` with an explicit state field (loading / typing / done) rather than a model per screen - three screens do not pay for that indirection. `Init` returns a `tea.Cmd` that fetches and returns a `lessonMsg`; fetching inline in `Update` blocks the UI. *Visible:* the words render, `ctrl+c` quits.
3. **The accumulator.** The per-position record plus cursor, last-correct-keystroke time, and the `firstTryError` flag - a plain struct with methods, separate from the `tea.Model`, and unit-tested by feeding a scripted keystroke sequence and asserting both derived maps. This is the one piece of chunk 2 that earns real test coverage, because it is the piece encoding a spec rule. *Visible:* the counters move with how you type.
4. **Submit.** Derive the two maps, POST, render `wpm` and `accuracy` from the 201. *Visible:* `GET /progress` before and after shows a key's score move.

## Chunk 3 - The demo

- **Fix the README first.** Its Quickstart currently advertises `ssh typist.gyarmathy.co`, which is phase 8 and does not exist. A reviewer's first action on a portfolio repo is to try the quickstart, and the first thing this one does is fail. Remove it until the SSH surface is real.
- `docker compose -f deploy/docker/compose.yaml up` already brings up app + Postgres (phase 1). The TUI runs locally against it; containerising the client is not needed for a demo and can wait.
- Record an asciinema of one lesson and link it in the README's `## Demo` section, which is currently a `TODO(phase-10)` placeholder.

**Done when:** a clean clone → `docker compose up` → `go run ./cmd/tui` → a played lesson, and that sequence exists as a recording in the README.

## Deferred, with the trigger for each

Nothing here is cancelled. Each has a condition that makes it worth doing, so none of it depends on remembering.

| Deferred                                    | Pick it up when                                                                    |
| ------------------------------------------- | ---------------------------------------------------------------------------------- |
| Concurrency test (phase 4 step 7)           | Immediately after the demo exists. The `psql` reproduction is already done, so this is an hour and it is interview gold. |
| `GET /sessions` + its composite index       | The history screen exists to read it (phase 5 proper).                              |
| Error-state _rendering_ (force-correction itself landed in chunk 2) | The typing _feel_ starts to matter - it is a genuine differentiator, not polish. The input model is done; what is left is showing the rejected keystroke. |
| Keyboard heatmap                            | After the loop is playable; it is the best screenshot in the project, so do not skip it, just do not block on it. |
| `$XDG_STATE_HOME` token storage             | You run the client on a second machine, or the env var becomes annoying.            |
| Engine tuning (`targetRaiseScore`, `allMastered`) | You have your own real session history to tune against - which chunk 2 is what produces. |
| SSH surface, refresh tokens, offline engine, trigrams | Already "beyond the slice" in the roadmap. Unchanged.                     |

## What this does not compromise

This plan defers **additions**. It lowers no bar on anything already written, and it removes no test that exists. The engine keeps its property tests and its harness; the API keeps its generated contract and its drift test; auth keeps its integration tests. A reviewer who opens `internal/engine` finds exactly what they would have found anyway.

The one thing it genuinely trades away is _completeness of the client_ at the moment of first demo: one screen, no history, no heatmap, no login. That is a smaller claim than the roadmap's phase 5 and the README should make it accurately.

It no longer trades away _correctness_ of the client. The 2026-09-04 decisions above pulled force-correction and ngram attribution back into chunk 2 on the finding that both were cheaper inside the input model than beside it, so what the client submits matches [`../engine.md`](../engine.md)'s attribution spec rather than approximating it.

## Open questions

- ~~**Whether the demo should wait for force-correction.**~~ Answered 2026-09-04: it does not wait, because it does not have to. Force-correction is a smaller `Update` branch than free typing, so chunk 2 implements it and the spec-exactness caveat the README was going to need never arises. Reasoning in chunk 2 above.
- **Whether `cmd/sshd` survives at all.** It is in the architecture doc and two ADRs, and it is the flashiest thing in the project (`ssh` a URL, get a typing trainer). It is also an entire second delivery surface. Worth deciding deliberately once the demo exists, rather than letting it drift.
