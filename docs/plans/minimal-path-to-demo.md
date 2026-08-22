# Minimal path to a demo

> The shortest honest route from where the code is today to thirty seconds of video showing this thing work. It reorders [`../roadmap.md`](../roadmap.md) rather than replacing it: nothing here is deleted from the roadmap, it is moved behind the demo. Written 2026-08-22, after phase 4's step 3.

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

- Token from a `TYPIST_TOKEN` environment variable.
- `GET /lessons/next`, render the words.
- Capture keystrokes; accumulate per-key `{attempts, errors, total_millis}`.
- `POST /sessions`; show the returned WPM and accuracy.

**Done when:** `go run ./cmd/tui` against a locally running server plays one lesson end to end and the score moves.

Explicitly out, all of it phase 5 proper: force-correction input, the keyboard heatmap, a polished results screen, a login screen, `$XDG_STATE_HOME` token storage, the anonymous/offline in-process engine.

Two consequences of this cut, both worth stating out loud rather than discovering:

- **Submit keys only; leave `Ngrams` empty.** `engine.ApplyResult` iterates the submitted ngram map, so an empty one simply updates nothing on that axis. Key unlocking and the target-WPM raise both read key scores, so the visible loop works. The cost is that ngram competency stays flat and the tier never advances - fine for a demo, and closing it later is one loop over adjacent character pairs.
- **Without force-correction the observations are approximations.** [`../engine.md`](../engine.md)'s client observation model specifies how an error is attributed when the typist is not forced to correct it. A v1 that lets the typist run on has to pick something simpler, so the numbers are directionally right rather than spec-exact. That is acceptable for a demo and dishonest to leave unsaid in the README.

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
| Force-correction input                      | The typing _feel_ starts to matter - it is a genuine differentiator, not polish.    |
| Keyboard heatmap                            | After the loop is playable; it is the best screenshot in the project, so do not skip it, just do not block on it. |
| `$XDG_STATE_HOME` token storage             | You run the client on a second machine, or the env var becomes annoying.            |
| Engine tuning (`targetRaiseScore`, `allMastered`) | You have your own real session history to tune against - which chunk 2 is what produces. |
| SSH surface, refresh tokens, offline engine, trigrams | Already "beyond the slice" in the roadmap. Unchanged.                     |

## What this does not compromise

This plan defers **additions**. It lowers no bar on anything already written, and it removes no test that exists. The engine keeps its property tests and its harness; the API keeps its generated contract and its drift test; auth keeps its integration tests. A reviewer who opens `internal/engine` finds exactly what they would have found anyway.

The one thing it genuinely trades away is _completeness of the client_ at the moment of first demo. A single-screen TUI with approximate attribution is a smaller claim than the roadmap's phase 5. Making that claim accurately in the README costs one sentence and is worth more than the missing features.

## Open questions

- **Whether the demo should wait for force-correction.** Recommendation: no. But the recording and the README should say what the client does today, because "the observations match the engine's attribution spec" is a phase 5 done-when and it will not be true yet.
- **Whether `cmd/sshd` survives at all.** It is in the architecture doc and two ADRs, and it is the flashiest thing in the project (`ssh` a URL, get a typing trainer). It is also an entire second delivery surface. Worth deciding deliberately once the demo exists, rather than letting it drift.
