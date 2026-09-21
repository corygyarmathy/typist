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

### Settled: slice 2 performs the split

**Head of slice 2, as its first commit.** Settled 2026-09-20, at the head of
slice 1. The decision above is _that_ it splits, not _when_. Two candidate
points, and this one was cheap enough to settle in front of the code:

- **Head of slice 1.** Honours "split first" literally. But slice 1 touches only
  the typing flow, so the split separates `loading` / `typing` / `done` from each
  other and nothing else - real work, since the results screen is a screen, but
  the second *independent* screen does not exist yet.
- **Head of slice 2**, as its first commit. The auth screen is the second
  independent screen, so the split is separating things that genuinely differ,
  and slice 1 stays a small focused diff. This is a pre-committed point, not
  "split when it hurts" - the distinction being that the trigger is named now.

The second won. Slice 1 therefore keeps the single `model` with its `state`
field and owns `width` directly; slice 2's root inherits that field as the last
known window size it passes to every screen it constructs.

## Slice 1 decisions, 2026-09-20

Settled in front of the code, as the rule above intends. Recorded here rather
than in an ADR because none is an architectural choice ([AGENTS.md](../../AGENTS.md#decision-capture):
records scale to the size of the decision). Two of them - the first and the
second - reach past slice 1, which is why they are written down at all.

### `Press` reports rejection; the model holds the rejected key

`accumulator.Press` returns a `bool`, and `model` stores the rejected rune.
Two alternatives were rejected. A `rejected rune` field on `accumulator` puts
display state in the struct whose job is building the `openapi.SessionSubmission`.
Comparing the key against `acc.text[acc.cursor]` in `Update` gives two places
that define what "correct" means and must agree forever. Returning a `bool`
leaves one definition of correct in `Press` while display state stays in the
display layer - which matters precisely because the model splits per screen in
slice 2.

The rejection is cleared by the next keypress, whatever it is: a second wrong
key replaces it, a correct key clears it. A timeout might read better, at the
cost of a `tea.Tick` and a message type; revisit only if the rule proves wrong
in use.

### `charmbracelet/lipgloss/v2` for styling

bubbletea v2 takes styling as ANSI escapes embedded in `tea.View.Content`, so
something has to produce them. Raw escapes need no dependency but emit colour
into terminals that do not want it; `charmbracelet/ultraviolet` is already an
indirect dependency but is bubbletea's internal rendering layer, not a styling
API.

lipgloss is the styling companion to a framework already chosen, from the same
module family, and its transitive dependencies (`colorprofile`, `x/ansi`,
`go-colorful`, `x/term`) are already indirect entries in `go.mod` via bubbletea -
so the added closure is essentially lipgloss itself. That is the justification
AGENTS.md's _standard library first_ asks for; it is not ADR-sized.

Constraint on the rendering: **do not distinguish by colour alone.** The
first-try-error positions carry a non-colour attribute (underline or reverse)
as well, so the distinction survives a monochrome terminal and a colour-blind
reader.

### The caret is the terminal cursor, not a drawn row

`tea.View.Cursor` is set to `tea.NewCursor(x, y)` and the `^` row is gone. The
caret sits _on_ the next character rather than under it, blink and shape come
from the terminal, and the position is assertable in a test without parsing
rendered text. It removed code rather than adding it.

The cost is one thing to keep straight: `tea.Cursor.Position` is relative to the
top-left of the **frame**, not of the text block, so anything rendered above the
text shifts the caret down. `View` derives that offset from the header it just
rendered rather than writing a literal, so the two cannot drift apart.

### Wrapping is greedy word wrap, expressed as line-start indices

`lineStarts(text []rune, width int) []int` returns the rune index at which each
wrapped line begins. Indices rather than `[]string` because the caret needs the
arithmetic anyway - row is the last start at or before the cursor, column is the
difference - and slicing between consecutive starts partitions the text, so no
rune is dropped.

Hard wrapping at `width` was rejected: splitting a word mid-word reads wrong in
a typing test. Two consequences accepted knowingly: searching a window of
`width` (not `width + 1`) keeps the line-terminating space inside the line's
budget at the cost of breaking one word early where a line would have fitted
exactly, and `width <= 0` - true for the frame before the first
`tea.WindowSizeMsg` arrives - returns a single unwrapped line rather than
falling back to a magic 80. The guard is also the loop's termination proof.

## Slice 2 decisions, 2026-09-21

Settled before session A's first line rather than in front of the code, because
every one of them reaches across more than one of the four sessions below and
retrofitting any of them mid-slice is the expensive outcome. None is an
architectural choice, so none gets an ADR ([AGENTS.md](../../AGENTS.md#decision-capture)).

### Five screens: loading, typing, results, login, register

`stateLoading` / `stateTyping` / `stateDone` each become a `tea.Model`, and the
auth work adds two more rather than one.

Loading is a screen even though it handles no input, because the alternative -
a `loading bool` inside the typing screen - gives the typing screen a second
mode in the same breath as the `state` field is deleted for having modes. It
also owns the `NextLesson` command, which is the thing that ends the loading
state, so command and state stay together.

Login and register are separate screens rather than one screen with a mode
toggle. The two collect the same two fields, so a toggle is tempting; what
differs is the meaning of a 409 and of a password typo, and a screen that has
to branch on its own mode to say what went wrong is the `state` field again at
a smaller scale. Attempting a login and falling back to register on a 401 was
rejected outright: it silently creates an account from a mistyped email.

### The root stores a local `screen` interface, not `tea.Model`

`tea.Model.Update` returns `tea.Model`, so a root field of that type forces a
type assertion on every single update - the exact cost the transitions-by-
message mechanic was chosen to avoid.

A local interface whose `Update` returns `screen` removes it. Nothing is
invented: the interface is `tea.Model`'s three methods with one return type
narrowed, so it is still the framework's shape rather than a design of our own.

### The token file is JSON: the token and an absolute expiry

`openapi.TokenResponse` carries `expires_in` in seconds, which is meaningless
once written to disk - it is relative to a moment the next process does not
know. Storing the raw JWT alone would therefore throw away the only expiry
information available at the point it is still interpretable, and the client
would learn about expiry by sending a request that is already doomed.

So the file holds the token plus the absolute time that `expires_in` resolves
to at the moment of the response, and startup can route to the login screen
without a round trip. The JWT's own `exp` claim is the server's authority and
this cache does not override it; a token this file believes is live can still
come back 401, which is what the typed error below exists to handle.

Mode `0600`, because the file is a bearer credential.

### The token store lives in `cmd/tui`, package `main`

Nothing outside the client reads a token off disk - the server issues tokens and
verifies them, and never loads one. A package under `internal/` for a single
consumer is the extraction AGENTS.md's _second or third real occurrence_ rule
exists to prevent, and the existing `cmd/tui` tests are already in package
`main`, so the store loses no testability by staying beside its only caller.

### `charmbracelet/bubbles/v2` for the text inputs

Two fields with masking, backspace, and a cursor is roughly forty lines
hand-rolled, so this is not a capability we cannot write. It is bought for the
edge cases a hand-rolled field gets wrong quietly: paste, word-delete,
navigation within the value, and the cursor reporting that slice 1 just settled
for the typing screen.

The honest cost, and it is larger than lipgloss's was: bubbles pulls seven
modules `go.mod` does not already carry - `MakeNowJust/heredoc`,
`atotto/clipboard`, `aymanbagabas/go-udiff`, `charmbracelet/harmonica`,
`x/exp/golden`, `dustin/go-humanize`, `sahilm/fuzzy` - because the module ships
every component together and only `textinput` is wanted. They are build-graph
entries rather than linked code, but they are entries all the same, and this
slice is the point at which that trade was accepted knowingly.

### The client returns a typed error carrying the status code

`errorFromResponse` flattens every status into a `fmt.Errorf` string today, so
the root cannot tell a 401 from a 500 without matching on text. Slice 2's
last session needs exactly that distinction.

A typed error carrying the status code rather than a bare `ErrUnauthorized`
sentinel, because the next two slices want the same discrimination for other
codes - slice 3's `400` on a forged cursor is already named in this plan - and a
second sentinel would be the first sign the shape was wrong. `errors.As`
recovers the code; the message stays as it reads today.

This is also where the carried-over review note below is repaid: `deref` is
fixed in the same session, since a `problem+json` body without a `detail` is
most likely on precisely the auth failures being added.

## Slice 2 implementation structure

Four sessions, one branch, one PR, in order. Each is sized to survive two or
three rounds of review without the next session's work being blocked behind
those rounds.

### Session A - the split, and nothing else

The settled split, as the slice's first commit. `cmd/tui/model.go` becomes a
root model plus the loading, typing, and results screens; the root owns `ctx`,
the `*Client`, and the last known window size it passes to every screen it
constructs. `cmd/tui/model_test.go` is redistributed, not extended.

No new behaviour. A review round on a file that only moved is fast; a review
round on a file that moved and changed is not, and that is the entire reason
this is a session of its own.

**Done when:** `make test` passes and the loop plays exactly as it did before
the commit.

### Session B - client and storage, no TUI

`Client.Register` and `Client.Login` against `POST /api/v1/auth/register` and
`POST /api/v1/auth/login`; the typed status error; the `deref` fix; the token
store. Every line is reachable from `httptest` and `t.TempDir()`, with no `tea`
involvement, so a long review here blocks nothing.

**Done when:** a test registers against a fake server, writes the token, reads
it back from a fresh store, and a `problem+json` body with no `detail` produces
an error instead of a panic.

### Session C - the auth screens and the startup path

The login and register screens, and the root wiring: a live token on disk goes
straight to loading, no token or an expired one goes to login, and the
`loggedInMsg` both writes the file and swaps in the new `*Client`.

**Done when:** a fresh checkout with no environment variables set can register,
play a lesson, quit, and restart straight into typing - the slice's own done
condition, minus the 401 path.

### Session D - the 401 return path and the README

A 401 mid-session returns to the login screen rather than an error string, and
`README.md` loses the `curl`-into-`TYPIST_TOKEN` step at line 32 along with the
note that the token does not persist.

**Done when:** an expired token mid-session lands the user on the login screen
and the lesson resumes after re-authenticating, and the README's quickstart no
longer mentions `TYPIST_TOKEN`.

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
