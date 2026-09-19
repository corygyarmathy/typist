# Phase 5 plan - the standalone TUI client

> What is left between the recorded demo and a client a reviewer could actually
> use. It finishes [`../roadmap.md`](../roadmap.md)'s phase 5 and collects the
> items the [minimal path to a demo](minimal-path-to-demo.md) deferred with a
> trigger. Written 2026-09-19, after phase 4 closed and the demo was recorded.

## The rule this document is written under

The same one the [minimal path](minimal-path-to-demo.md#the-rule-this-plan-is-written-under)
established, and for the same reason: phase 4's plan was detailed enough to
follow without thinking, which turned building into transcription and stopped it
being learning.

So this document states **what each slice is, what _done_ means, and what has to
be decided before the first line** - and stops. Decisions inside a slice get made
at the time, in front of the code. Where a decision genuinely spans slices it is
listed below as a question with its options, not as an answer; the answer
originates with the author.

## Where we are

Chunk 2 of the minimal path landed a working single-screen client: one
`tea.Model` with a `state` field, `cmd/tui/client.go` against the generated
`internal/openapi` models, and `cmd/tui/accumulator.go` implementing
force-correction plus spec-exact per-key and per-bigram attribution. The loop is
playable and recorded.

What it is not is a client. There is no way to get a token except
`TYPIST_TOKEN`, a rejected keystroke is invisible, nothing reads the history the
`sessions` table has been accumulating since phase 4, and the competency state
the engine works hardest on is never drawn.

## Definition of done

Unchanged from the roadmap: **the full loop is playable against the local
server, the observations the client submits match the engine's attribution spec,
and the history screen pages through real sessions.** The middle clause is
already true as of chunk 2.

## The slices

Five, deliberately loosely coupled, each a session and a PR of its own. Only
slice 5 has a hard predecessor.

### Slice 1 - Error-state rendering and the caret

The typing _feel_, which the deferral table calls a genuine differentiator
rather than polish. The input model is done; what is missing is showing the
rejected keystroke.

In scope: render the rejected key, the already-typed prefix and the
first-try-error positions distinctly; handle `tea.WindowSizeMsg` so the caret
lands under the right character once the line wraps (today `View` draws it as
`strings.Repeat(" ", cursor) + "^"`, which is correct only while the text fits
on one line).

**Done when:** typing a wrong key is visibly rejected rather than silently
ignored, and the caret stays aligned in a narrow terminal.

Why first: it is the smallest, it touches only `cmd/tui/model.go`, and every
lesson played from here on feeds slice 5.

### Slice 2 - Auth screen and `$XDG_STATE_HOME` token storage

Removes `TYPIST_TOKEN` from the README's instructions.

In scope: a login/register screen; `POST /auth/login` and `POST /auth/register`
on the client; the token persisted under `$XDG_STATE_HOME` (falling back to
`~/.local/state` per the XDG spec) and loaded at startup; a 401 mid-session
returning the user to the login screen rather than an error string.

**Done when:** a fresh checkout with no environment variables set can register,
play a lesson, quit, and restart straight into typing.

### Slice 3 - `GET /sessions`, migration `0005`, and the history screen

The only slice with a server half, and the one deferral whose reasoning is
already done.

In scope: [Decision 5 of the phase 4 plan](phase-4-sessions.md#5-keyset-pagination-via-two-named-queries-not-one-query-with-nullable-parameters-deferred-to-phase-5)
verbatim - the two named queries, the `base64url(RFC3339Nano + "|" + uuid)`
cursor, `ParseCursor` / `ErrInvalidCursor`, the `400` on a malformed cursor,
`limit` defaulting to 20 in the handler - plus the composite index and the
screen that reads it. The route is already declared in `api/openapi.yaml` with
its `SessionPage` schema and answers `501` today.

**The index goes in a new `migrations/0005_*.sql`.** `0004` is applied and
stands exactly as written; the index arrives with the query that needs it, which
was the whole argument for deferring it.

**Done when:** the history screen pages through more sessions than fit on one
page, an integration test covers the page boundary including two sessions
sharing a `completed_at`, and a forged cursor returns a `problem+json` 400.

Do not re-derive Decision 5. Read it, satisfy yourself it is right, and
implement it.

### Slice 4 - Progress screen and the keyboard heatmap

The best screenshot in the project, per the deferral table - so do not rush it,
and do not let it block anything.

In scope: `GET /progress` on the client, and a screen rendering the
`Competency` document. The heatmap colours each key by its score. Two things the
schema is explicit about and the rendering must not blur: `keys` is the **unlock
set**, so absence means locked; `ngrams` is a **score cache**, so a missing entry
means "in scope but never practised", not "unavailable".

**Done when:** the heatmap distinguishes locked, unlocked-but-unpractised, and
scored keys, and a key's colour visibly moves after a lesson.

### Slice 5 - Engine tuning

The two defects carried from phase 3, which phase 4 deliberately refused to
touch because [a robot submitting fabricated observations cannot say whether a
setpoint is wrong for a person](phase-4-sessions.md#engine-tuning-is-out-of-scope-and-why).

In scope: `targetRaiseScore` (0.85, which parks every score at ~0.82) and the
`allMastered` AND-quantifier in `internal/engine/progression.go`. Read the phase
3 open questions first. The underlying question is a dynamics one - should the
target be allowed to fall? should `targetRaiseScore` sit above
`unlockKeyThreshold`? - and it deserves its own focused session, not a drive-by
constant edit.

**Blocked on:** your own real session history, which slices 1-4 are what
produce. This is the one ordering constraint in the phase. Slice 3 is also how
you read that history back without `psql`.

**Done when:** the constants are justified by data you generated, and the
justification is recorded - a doc comment or a test name asserting the
invariant, not an ADR ([AGENTS.md](../../AGENTS.md#decision-capture): records
scale to the size of the decision, and a constant is not an architectural
choice).

## Decisions, 2026-09-19

Both span the slices, so both were settled before slice 1 rather than in front of
the code - retrofitting either mid-phase is the expensive outcome.

### One screen per `tea.Model`

The single model with a `state` field does not survive this phase, and it is
split at the point the second screen arrives rather than carried and unpicked
later.

Chunk 2's reasoning (_three screens do not pay for that indirection_) was sound
for three screens. It does not extend here, and neither does
[AGENTS.md](../../AGENTS.md#engineering-principles)'s _extract on the second or
third real occurrence_: that rule guards against **not knowing the shape yet**,
so that any abstraction chosen is a guess. Neither half of that holds. The
screens are enumerated, and the interface is not invented - `tea.Model` is
already exactly `Init() Cmd` / `Update(Msg) (Model, Cmd)` / `View() View`, so
there is no speculative design to get wrong. Building the known-wrong shape in
order to claim the rule was honoured would be the rule's letter against its
purpose.

Two mechanics follow, and they are the parts that actually cost something:

- **Transitions go through messages, not return values.** The login screen emits
  a `loggedInMsg`; the root catches it in its own `Update` and swaps the current
  screen. Screens stay ignorant of each other, which is the point of splitting,
  and it removes the type assertion that otherwise comes with `Update` handing
  back a `tea.Model`.
- **The root owns the last known window size.** `tea.WindowSizeMsg` is sent once
  at startup and then only on resize, so a screen constructed later - the history
  screen, mid-session - never receives one and renders at a width it cannot
  discover. The root stores the size and passes it to every screen it creates.
  This is slice 1's caret work meeting slice 3's new screens; it is far cheaper
  known than debugged.

### The client's token is immutable; login yields a new `*Client`

Not a question of taste. `tea.Cmd` is documented as "an IO operation" and the
runtime executes each one in its own goroutine, while `Update` runs on exactly
one. A `SetToken` mutating `client.token` while a submit `Cmd` is in flight is a
data race, and it is reachable inside slice 2: a 401 mid-session returns to the
login screen, re-authenticates, and retries. `make test` runs `-race`, so it
would be caught - after it had been built on.

So the token stays immutable, login constructs a **new** `*Client`, and the root
swaps the pointer in `Update`. No mutation and no lock, because the swap happens
on the single-threaded path and every in-flight `Cmd` keeps the pointer it
captured. A request already in flight under the old token completes as a 401,
which is the correct outcome rather than something to paper over. It matches the
framework's own concurrency model instead of working around it, and costs one
allocation per login.

Rejected: a `sync.Mutex` around the field (fixes the race, but puts a lock on a
struct with no other shared state and makes the concurrency read as harder than
it is), and a per-call `token` parameter (no shared state at all, but threads the
token through every call site and moves the token into the model).

### Open: which slice performs the split

The decision above is _that_ it splits, not _when_. Two candidate points, and
this one is cheap enough to settle in front of the code:

- **Head of slice 1.** Honours "split first" literally. But slice 1 touches only
  the typing flow, so the split separates `loading` / `typing` / `done` from each
  other and nothing else - real work, since the results screen is a screen, but
  the second *independent* screen does not exist yet.
- **Head of slice 2**, as its first commit. The auth screen is the second
  independent screen, so the split is separating things that genuinely differ,
  and slice 1 stays a small focused diff. This is a pre-committed point, not
  "split when it hurts" - the distinction being that the trigger is named now.

## Carried-over review note

`cmd/tui/client.go`'s `deref` dereferences unconditionally, but
`openapi.Problem.Detail` and `.Instance` are `*string` and optional in the spec.
A `problem+json` body without a `detail` panics the client inside its own error
path. Harmless while every error the server returns happens to set one; slice 2
adds auth failures, which is exactly where a terse problem body is likely.

## Out of scope, still

Unchanged from the roadmap's _beyond the slice_ and the minimal path's deferral
table: the SSH surface, refresh tokens, the anonymous/offline in-process engine,
and trigrams. Also still open, and worth deciding once this phase makes the
client real: **whether `cmd/sshd` survives at all.**
