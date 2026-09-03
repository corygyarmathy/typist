# Phase 4 implementation plan - the progress & sessions vertical

> A granular, session-to-session build plan for phase 4 of [`../roadmap.md`](../roadmap.md). The roadmap says _what_ phase 4 is and _when_ it's done; [`../architecture.md`](../architecture.md) has the write flow; [`../schema.md`](../schema.md) has the tables and the cursor rule; this says _in what order we build it_ and _why each decision was made_. It is a living plan - update it as we go.

## How this phase is different (read first)

Phase 3 was pure functions with no network edge, so the failing **test** was the driver. Phase 4 has an HTTP edge again, so **phase 2's driver comes back**: write the contract, generate, stub to 501, `curl` the route, and let each failing call pull the next file into existence. That is the mode that worked before; use it.

What is genuinely new - and it is only one thing - is **`POST /sessions`**: the first request that writes across two bounded contexts inside one transaction, and the first that accepts a rich body from an untrusted client. Everything else in this phase is a read you already know how to build. Budget accordingly: `GET /lessons/next` and `GET /sessions` are an afternoon each; `POST /sessions` is the phase.

**Engine tuning is not in this phase.** See [Engine tuning is out of scope](#engine-tuning-is-out-of-scope-and-why) - this matters enough to be its own section, because the carry-over note from phase 3 points here and the honest answer is "not yet."

## Where we are

- Branch to build on: `main` (phases 1-3 complete and merged, 2026-08-20).
- **The `sessions` table already exists.** `migrations/0004_create_sessions_table.sql` was written in phase 1 and is applied. The roadmap's "sessions migration" bullet is therefore already done _except_ for one defect: the migration creates `CREATE INDEX sessions_completed_at_idx ON sessions (completed_at)`, but [`../schema.md`](../schema.md) specifies `(user_id, completed_at DESC)` - the keyset query filters by `user_id` first, so the existing index cannot serve it. Phase 4 adds `0005` to fix that; it does **not** edit `0004`.
- **The stubs to fill in** (all currently one-line `TODO(phase-4)` comments):
  - `internal/session/handler.go`, `service.go`, `repository.go`, `models.go`
  - `sqlc.yaml` - the session block exists, commented out. Uncomment it.
- **The seams that already work and stay untouched:** the auth middleware, `publicRoutes` in `cmd/server/router.go` (new routes are protected by default - a forgotten entry is a 401, not an open endpoint), `TestRouter_SpecDrift`, and the `errNotImplemented` / `ResponseErrorHandlerFunc` error mapping.
- **The pattern to copy for the cross-context write** is already in the repo: phase 2's `auth.ProgressInitialiser` + `func(pgx.Tx) auth.ProgressInitialiser` factory, wired in `cmd/server/wiring.go`. `POST /sessions` is the same shape with a different interface. Read `internal/auth/service.go:Register` before starting step 5 - it is the transaction skeleton (begin, deferred rollback, tx-bound repositories, commit) you will reproduce.
- **`api/openapi.yaml:42-55`** carries a comment listing the phase-4 surface, and `Competency` at line 244 is still `type: object` with a "finalised in phase 4" note. Both are this phase's work.

## Definition of done (from the roadmap)

> A scripted loop (register → next → submit → progress) shows competency changing across calls; the FOR-UPDATE write is covered by a concurrency test.

Concretely, phase 4 ships:

- [ ] `GET /api/v1/lessons/next` - loads competency, calls `engine.NextLesson`, returns words + targets. No writes.
- [ ] `POST /api/v1/sessions` - the transactional write: `SELECT … FOR UPDATE` → `engine.ApplyResult` → server-derived WPM/accuracy → insert session + update competency → commit.
- [ ] `GET /api/v1/sessions` - keyset-paginated history.
- [ ] `GET /api/v1/progress` - already serves; phase 4 finalises its schema in the spec.
- [ ] Migration `0005` - the `(user_id, completed_at DESC, id DESC)` index.
- [ ] `internal/session`: models, repository (+ sqlc), service, handler.
- [ ] `internal/progress`: `LoadForUpdate` / `Save` on a tx-bound store, plus the two queries.
- [ ] Unit tests for the pure derivation, service tests against fakes, a **concurrency test** proving no lost update, and an **e2e scripted loop** proving competency moves.

## Decisions (locked in for phase 4)

### 1. `GET /lessons/next` is served by the `progress` context, not `session`

[`../architecture.md`](../architecture.md)'s request flow already says so ("the progress handler … calls `progress.Service.NextLesson`"), and it is right: the only state the endpoint reads is `user_progress.competency`, which `progress` owns. A URL naming a noun (`lessons`) that no context owns is a little odd, and it is worth being able to say why out loud: **lessons are generated, never stored**, so there is nothing for a `lesson` context to own. The endpoint is a projection of progress state through a pure function.

Cost accepted: `progress.Service` now depends on `engine.Corpus`, so `progress` imports `engine` for two reasons rather than one. That import already exists (`InitialCompetency`).

### 2. The client submits observations only; the server derives WPM and accuracy from them

Request body is exactly the engine's `Result`: per-key and per-ngram `{attempts, errors, total_millis}`. **No `duration_millis` field**, and no client-computed `wpm` or `accuracy`.

The alternative - a separate client-reported duration - was rejected because it adds a second, independently-forgeable source for a number we can already compute, and the two can disagree. Deriving from the observations means the number the session row reports and the number the engine scored against come from the same input.

Derivation, as a pure function in `internal/session` (**keys only** - see the trap below):

```go
// derive computes the session summary from the submitted observations.
// chars, errs and millis all sum over res.Keys.
//   accuracy = 1 - errs/chars
//   wpm      = (chars / 5) / (millis / 60000)
func derive(res engine.Result) (wpm int, accuracy float64, err error)
```

> **The trap: never sum `res.Ngrams` into these totals.** A bigram's `TotalMillis` covers two keystrokes, and a middle character sits in two bigram windows, so summing the ngram side double-counts both time and attempts. This is the same double-count the phase-3 harness hit (`harness_test.go`, decision 3); it is now a production bug rather than a test one.

Two consequences to state in code comments: word-boundary spaces are not scored as keys ([`../engine.md`](../engine.md#client-observation-model)), so `chars` is the lesson length _excluding_ spaces and WPM is very slightly conservative against the standard five-characters-per-word convention; and `wpm` truncates to `int` because the column is `INT`.

### 3. Validation rejects malformed input; the engine already ignores out-of-scope input

Two different jobs, and it is worth keeping them apart.

**The service validates shape**, returning `400 problem+json` on: any observation with `attempts < 1`, `errors > attempts`, `errors < 0`, or `total_millis <= 0`; an empty `keys` map; a key that is not exactly one rune; or more than `maxSubmittedItems` entries across the two maps (propose 1000 - the 1 MiB global body cap is not a bound on map cardinality). These are client bugs and the client should hear about them.

**The engine already handles scope**, and nothing needs adding: `ApplyResult` skips any key not already present in `s.Keys` and any ngram outside `activeNgrams`. So a client that submits a locked key gets a `201` and no competency change for that key. That silent drop is the right default - the client must not be able to expand its own unlock set - but write it down, because "I submitted `z` and nothing happened" is otherwise a confusing report.

### 4. The cross-context seam is a consumer-owned interface plus a tx-bound factory (phase 2's pattern)

`internal/session/service.go` declares what it needs from `progress`; `cmd/server/wiring.go` supplies it. `session` never imports `internal/progress/db`.

```go
// In internal/session/service.go - consumer-owned, so the boundary stays real.
type CompetencyStore interface {
    LoadForUpdate(ctx context.Context, userID uuid.UUID) (engine.CompetencyState, error)
    Save(ctx context.Context, userID uuid.UUID, s engine.CompetencyState) error
}

type Service struct {
    pool        *pgxpool.Pool
    repo        Repository                       // pool-bound, for the read path
    newRepo     func(pgx.Tx) Repository          // tx-bound, for the write path
    newProgress func(pgx.Tx) CompetencyStore
    corpus      engine.Corpus
    now         func() time.Time
}
```

**The interface speaks `engine.CompetencyState`, not `[]byte`.** JSON encoding is a persistence detail and `progress` already owns it (it marshals the initial document at registration). Keeping it there means `internal/session` never imports `encoding/json` for competency at all. `progress.GetProgress` keeps its `[]byte` passthrough - that is a separate method serving a separate endpoint.

`internal/progress` gains a `Store` alongside the existing `Initialiser`:

```go
func NewStore(tx pgx.Tx) *Store   // tx-bound only; there is no pool-bound Store
```

Tx-bound only is deliberate: `LoadForUpdate` issues `SELECT … FOR UPDATE`, and a row lock outside a transaction is released immediately, which would be a silently useless lock. Making the constructor take `pgx.Tx` means that mistake does not compile.

### 5. Keyset pagination via two named queries, not one query with nullable parameters

```sql
-- name: ListSessionsFirstPage :many
SELECT id, wpm, accuracy, completed_at FROM sessions
WHERE user_id = $1
ORDER BY completed_at DESC, id DESC
LIMIT $2;

-- name: ListSessionsAfter :many
SELECT id, wpm, accuracy, completed_at FROM sessions
WHERE user_id = $1 AND (completed_at, id) < ($2, $3)
ORDER BY completed_at DESC, id DESC
LIMIT $4;
```

One query with `($2::timestamptz IS NULL OR …)` is possible, but the `OR NULL` form is harder to read and gives the planner a predicate it may not push into the index. Two queries are five lines longer and both are obviously index-only.

The row-comparison `(completed_at, id) < ($2, $3)` is the point of the composite index and the reason `id` is in the cursor: `completed_at` alone is not unique, so a tie at a page boundary would either repeat or skip a row.

**Cursor format:** `base64url(RFC3339Nano + "|" + uuid)`, opaque to the client, decoded in the handler. A malformed cursor is a `400`. It is **not** signed and does not need to be: every query is scoped by the `user_id` the middleware put in the context, so the worst a forged cursor can do is move the caller's own window.

`limit` is a query parameter, `1..100`, default `20`.

### 6. `accuracy` stays `NUMERIC`; the repository converts

sqlc emits `pgtype.Numeric` for the column. Convert to `float64` in `internal/session/repository.go` via `Float64Value()`, and back on insert. It is about five lines each way.

Rejected: a sqlc `overrides` entry mapping `numeric` → `float64` (hides a lossy conversion at a layer where nobody will look for it), and migrating the column to `DOUBLE PRECISION` (churn on an applied migration, and `NUMERIC` plus the existing `CHECK (accuracy >= 0 AND accuracy <= 1)` is the more honest column for a ratio).

### 7. A fresh `*rand.Rand` per request

`math/rand/v2`'s `*rand.Rand` is **not safe for concurrent use**. A single generator on `progress.Service` shared across HTTP handlers is a data race, and `go test -race` will find it - but only if a test happens to hit the endpoint concurrently, so do not rely on that.

```go
// Default; tests inject a fixed seed for a deterministic lesson.
newRand: func() *rand.Rand { return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) }
```

The top-level `rand.Uint64()` **is** goroutine-safe, so seeding this way needs no lock. A `sync.Mutex` around one shared generator or a `sync.Pool` both work too; per-request is cheaper than the pool and simpler than the mutex, and it is the only one of the three that lets a test pin the seed by swapping the field.

### 8. `POST /sessions` returns the session summary only, not the updated competency

The client calls `GET /progress` afterwards to render the results screen. One extra round trip, in exchange for a response that means one thing. The updated state _is_ in hand at commit time, so folding it into the response is a two-line change if phase 5 finds the round trip annoying - listed under [nice-to-haves](#nice-to-haves-circle-back-later).

## Engine tuning is out of scope, and why

The carry-over note from phase 3 says: read the phase-3 open questions before touching engine tuning, and let phase 4's real feedback loop settle `targetRaiseScore` (which parks every score at ~0.82) and the `allMastered` AND-quantifier, rather than guessing beforehand.

The first half stands. The second needs correcting, and it is better corrected now than discovered in step 9: **phase 4 does not produce a feedback loop that says anything new about those constants.** The DoD's scripted loop is a robot submitting fabricated observations - the phase-3 harness again, only slower and over HTTP. It exercises the plumbing; it cannot tell you whether 0.82 is a bad setpoint for a person, because there is no person in it. The human feedback loop arrives in **phase 5**, when the TUI puts real fingers on real lessons.

So: **the `const` block in `internal/engine/engine.go` does not change in phase 4.** Not one number. Two reasons beyond the above - a constant changed now moves the ground under every phase-4 test fixture you are about to write, and the ratchet defect is a _dynamics_ problem whose fix (should the target be allowed to fall? should `targetRaiseScore` sit above `unlockKeyThreshold`?) deserves its own focused session with real data, not a drive-by edit during plumbing work.

What phase 4 _should_ do is make that session possible: the `sessions` table becomes the first real record of what a learner actually did, and `GET /sessions` is how you will read it back. That is the contribution. Revisit the tuning at the top of phase 5, with a human's session history in hand.

## The build order (outside → in)

### Step 1 - Write the contract

Add to `api/openapi.yaml`. Delete the now-satisfied planning comment at lines 42-55 as you go.

New paths (all default-protected, no `security: []`):

- `GET /api/v1/lessons/next` → `200 Lesson`, `401`
- `POST /api/v1/sessions` → `201 SessionSummary`, `400`, `401`
- `GET /api/v1/sessions` (`cursor`, `limit`) → `200 SessionPage`, `400`, `401`

New schemas:

- `Lesson {words[], targets[]}`,
- `Observation {attempts, errors, total_millis}`,
- `SessionSubmission {keys, ngrams}` (both `additionalProperties: Observation`),
- `SessionSummary {id, wpm, accuracy, completed_at}`,
- `SessionPage {sessions[], next_cursor?}`.

And **finalise `Competency`**, which is still the phase-2 placeholder `type: object`: `{keys, ngrams, ngram_tier, target_wpm}` with a new `ItemScore {score, samples, last_practiced}`. Restate it from [`../schema.md`](../schema.md), not from the Go type - the point of the spec is to be an independent statement of the shape, the same argument `marshal_test.go` made in phase 3.

Put the `minimum` / `maxLength` bounds in the spec even though nothing enforces them yet. They document the contract, and the deferred `nethttp-middleware` item in the roadmap is what will make them load-bearing.

**Checkpoint:** `make openapi` regenerates cleanly and the new `StrictServerInterface` methods are visible.

### Step 2 - Route the skeleton

Add the three methods to `cmd/server/api.go` returning `errNotImplemented`, delegating to handlers that do not exist yet (comment them out, or point them at the existing handler types with a TODO). **Checkpoint:** `curl` each new route with a valid bearer token → `501 problem+json`; without a token → `401`. That second one is the proof that default-deny worked and `publicRoutes` needed no edit.

**Found while doing it: two 400s never reach `*API`, so the sentinel switch in `router.go` cannot map them.** oapi-codegen owns them, and both defaulted to `http.Error` - `text/plain` carrying the raw error - against a spec where every `400` is declared `application/problem+json`. Both options are now set in `Router`:

| Failure | Generated hook | Runs |
| --- | --- | --- |
| Body is not valid JSON for the operation | `RequestErrorHandlerFunc` | inside the strict handler, **after** `RequireAuth` |
| Query param will not bind (`?limit=abc`) | `ErrorHandlerFunc` | inside the route wrapper, **before** `RequireAuth` |

The ordering in row two is the real defect: an anonymous caller got a `400` naming a parameter of a route they were not authenticated for, where every other request to it answers `401`. It is not fixable by reordering (the generated wrapper binds parameters before applying `Middlewares`, and hoisting `RequireAuth` out of that chain would strand it above the mux, where `r.Pattern` is empty and every route including `/healthz` reads as protected). So `ErrorHandlerFunc` re-asks the gate itself, via a new `auth.Authenticate` extracted from `RequireAuth` - one decision, two callers, rather than a security check written twice.

Neither handler echoes the underlying error any more; both log it with the request ID and return a generic detail, naming only the parameter (which is published in the spec anyway). Three cases in `TestRouter` pin all of it, including that the unauthenticated bad-`limit` request is a `401`.

Note for step 6: `limit` and `cursor` are therefore already guaranteed well-typed by the time the handler sees them - `*int` and `*string`, nil when absent. What is left to validate there is range (`1..100`) and the default of `20`, neither of which the generated code applies.

### Step 3 - `GET /lessons/next` (the easy end of the vertical)

No migration, no new SQL, no transaction. It closes the engine↔HTTP seam and gives you a working endpoint fast.

- `progress.Service` gains `corpus engine.Corpus`, `newRand func() *rand.Rand` and `now func() time.Time` (Decision 7); `NewService` takes the corpus, `main.go` passes `corpusProvider`.
- `progress.Service.NextLesson(ctx, userID) (engine.Lesson, error)`: load competency → `json.Unmarshal` into `engine.CompetencyState` → `engine.NextLesson(state, s.corpus, s.now(), s.newRand())`.
- `progress.Handler.GetNextLesson` maps it to the generated response type.

**Prove it:** a service test with a fake repository and a fixed seed asserts the same lesson twice; an e2e test registers a user and asserts every character of every word is in the returned competency's `keys`. That is the roadmap's phase-3 invariant re-asserted at the HTTP boundary, which is where it now actually matters.

**Checkpoint:** register → `GET /lessons/next` → a lesson of `e/t/a/o` words only (the four starting keys).

### Step 4 - Migration `0005` and the session persistence layer

- `make migrate-new name=index_sessions_by_user`, then:
  ```sql
  CREATE INDEX sessions_user_completed_at_idx ON sessions (user_id, completed_at DESC, id DESC);
  DROP INDEX sessions_completed_at_idx;
  ```
  Down reverses both. Do **not** edit `0004`.
- Uncomment the session block in `sqlc.yaml`.
- `internal/session/queries.sql`: `CreateSession`, plus the two list queries from Decision 5. `make sqlc`.
- `internal/session/models.go`: `Session {ID, WPM, Accuracy, CompletedAt}`, a `Cursor` type with `Encode`/`ParseCursor`, and the error sentinels (`ErrInvalidObservation` → 400, `ErrInvalidCursor` → 400). Register them in `router.go`'s `ResponseErrorHandlerFunc`.
- `internal/session/repository.go`: `Repository` interface + `pgxRepository` over `db.DBTX` - the same shape as `auth`, and the reason one type serves both the pool and the tx.

**Prove it:** a repository integration test inserts three sessions and pages through them with `limit=2`, asserting no repeats and no gaps. Insert two rows with an **identical `completed_at`** on purpose - that is the case the `id` tie-break exists for and the one a naive cursor gets wrong.

### Step 5 - `POST /sessions`, the transactional write (the phase)

`internal/progress` first: add `GetUserProgressForUpdate` (`SELECT competency FROM user_progress WHERE user_id = $1 FOR UPDATE`) and `UpdateUserProgress` (`UPDATE user_progress SET competency = $2, updated_at = now() WHERE user_id = $1`) to `queries.sql`, `make sqlc`, then the `Store` from Decision 4.

Then `session.Service.Submit(ctx, userID, res engine.Result) (Session, error)`, in this exact order:

1. **validate** (Decision 3) and **derive** (Decision 2) - both pure, both _before_ opening a transaction. Nothing that can fail on client input should hold a row lock.
2. `tx, err := s.pool.Begin(ctx)`; `defer` the rollback exactly as `auth.Service.Register` does (ignoring `pgx.ErrTxClosed`).
3. `state, err := s.newProgress(tx).LoadForUpdate(ctx, userID)` - the lock.
4. `next := engine.ApplyResult(state, s.corpus, res, now)` - pure, no I/O, no error.
5. `sess, err := s.newRepo(tx).Create(ctx, userID, wpm, accuracy, now)`.
6. `s.newProgress(tx).Save(ctx, userID, next)`.
7. `tx.Commit(ctx)`.

Then the handler: decode the generated request type into an `engine.Result` (this is where `map[string]Observation` becomes `map[rune]Observation`, and where a multi-rune key is rejected), call the service, shape the response.

**Three things to get right, each of which is a real bug if you don't:**

- **One `now`.** Take `now := s.now()` once at the top and pass the same value to `ApplyResult` and to the session row. Two calls to `time.Now()` produce two timestamps and the row will not match the `last_practiced` the engine wrote.
- **`time.Time` does not survive a JSON round trip under `==`.** Marshalling strips the monotonic clock reading, so a state loaded from the database compares unequal to an in-memory one built from the same `time.Now()`. Phase 3 recorded this as a live trap for exactly this code path (see the note in `internal/engine/marshal_test.go`). Compare with `.Equal()`, and never assert `reflect.DeepEqual` on two states that have been through the database.
- **The lock is only real inside the transaction.** `LoadForUpdate` on a pool connection acquires and releases immediately. Decision 4's tx-only constructor is what prevents this; do not add a pool-bound variant "for symmetry".

**Checkpoint:** register → `GET /lessons/next` → hand-build a `Result` covering the lesson's keys → `POST /sessions` → `201` with a plausible WPM → `GET /progress` shows the samples went up.

### Step 6 - `GET /sessions`

Handler decodes `cursor` and `limit`, service calls the right query, response carries `next_cursor` only when a full page came back. Straightforward after step 4 - it is one branch and a base64 decode.

> Decide once and comment it: returning `next_cursor` whenever `len(rows) == limit` means the last page costs one extra empty request. The alternative is `LIMIT n+1` and returning `n`. Take whichever, but say which and why - it is the kind of small deliberate choice worth being able to defend.

### Step 7 - The concurrency test (the DoD's second half)

`internal/session/service_integration_test.go`. Shape:

1. Register a user.
2. Launch **N = 8** goroutines, each submitting the same single-key observation (`{'e': {Attempts: 10, Errors: 0, TotalMillis: 2000}}`), released together from a `sync.WaitGroup` barrier.
3. Wait, then assert `competency.keys['e'].samples == 80` and that `sessions` has exactly 8 rows for the user.

**The test proves nothing unless the pool is big enough.** `pgxpool`'s default `max_conns` is `max(4, NumCPU)`; if the goroutines queue on the pool instead of on the row lock, they serialise for the wrong reason and the test stays green with `FOR UPDATE` deleted. Open a pool with `MaxConns >= N` in this test explicitly.

**Confirm it red by mutation** - the phase-3 habit that made those tests trustworthy: delete `FOR UPDATE` from the query and re-run. Samples should land somewhere well under 80. If it stays green, the test is not testing what you think.

### Step 8 - The scripted loop e2e (the DoD's first half)

Extend `cmd/server/e2e_test.go`: register → `GET /lessons/next` → fabricate a `Result` from the returned words → `POST /sessions` → `GET /progress`, asserting competency moved; loop ~20 times and assert a **fifth key has unlocked** (the initial set is four).

The fabricated result does not need to be realistic - the phase-3 harness's `simulate` is an internal test helper and cannot be imported, and copying it here would be building a second simulator to maintain. This test wants a _known_ result, not a plausible one: perfect accuracy at a comfortable interval, derived from the words the server just sent. Keep it to about fifteen lines.

## Testing strategy

- **Pure unit, no DB:** `derive` (accuracy and WPM against hand-computed values; the ngram double-count guarded by a case where `Ngrams` is populated and the answer must not change); validation rejects each malformed shape; cursor encode/decode round-trips and rejects garbage.
- **Service, against fakes:** `Submit` with a fake `Repository` and fake `CompetencyStore` - asserts the order of operations and that a validation failure never begins a transaction.
- **Repository integration (needs `DATABASE_URL`):** paging including the identical-`completed_at` tie, and the `FOR UPDATE` behaviour.
- **Concurrency:** step 7.
- **E2e:** step 8.

Reuse the existing `newTestPool` / `uniqueEmail` / `cleanupUser` helpers and their conventions - DB tests skip when `DATABASE_URL` is unset, and isolate on a unique email rather than truncating shared tables.

## Documentation & tooling impact

- **`go.mod`:** expect **no new dependencies**. Everything needed is stdlib or already present (`pgx`, `pgtype`, `uuid`, `encoding/base64`).
- **`sqlc.yaml`:** uncomment the session block. No other change.
- **ADRs:** expect **none new.** The cross-context transactional write is already ADR 0003 (modular monolith) plus the write flow in `architecture.md`; JSONB competency is ADR 0009; keyset pagination is a schema-doc decision, not a cross-cutting opaque fork. Decisions 1-8 above are local structuring choices and belong here and in code comments. Keeping the ADR index from sprawling is itself the call to be able to defend.
- **`docs/schema.md`:** update the migration list with `0005` and note the corrected index name.
- **`docs/architecture.md`:** the write flow's step 3 says `engine.ApplyResult(state, result, now)` - it takes a corpus (corrected in phase 3). Fix while you are in there.
- **`api/openapi.yaml`:** the phase-4 planning comment at lines 42-55 and the "finalised in phase 4" note on `Competency` both get deleted once satisfied.

## Nice-to-haves (circle back later)

Deliberately out of phase 4. Each is an increment on something working, not a prerequisite.

1. **Return the updated competency in the `POST /sessions` response** (Decision 8) - saves phase 5 a round trip on the results screen. Two lines.
2. **A signed lesson token.** Nothing ties a submission to a lesson the server generated, so a client can submit any observations it likes. Harmless for a personal trainer where the only person cheating is the user; the fix if it ever matters is an opaque signed token issued by `/lessons/next` and echoed back.
3. **Idempotency on `POST /sessions`** - a client-supplied key so a retried submission does not double-count. Wanted the moment the TUI grows retry-on-timeout.
4. **`Location` header on the `201`** - correct REST manners, zero functional benefit here.
5. **A `scripts/` curl walkthrough of the loop** for the README, alongside the Go e2e test. Good demo material for phase 6.
6. **Per-session lesson text or observation storage** - a nullable `jsonb` column, per [`../schema.md`](../schema.md). Only once something wants replay or offline re-tuning.
7. **Spec-conformance testing** at runtime, and **spec-driven request validation** via `nethttp-middleware` - both already sized in the roadmap's "beyond the slice" section, and both get more valuable now that a real request body exists.
8. **`limit`-aware `LIMIT n+1` paging** if the extra empty request in step 6 turns out to annoy.

## Open questions / seams for later

- **`targetRaiseScore` and the `allMastered` quantifier** - both measured defects carried from phase 3, both deliberately untouched here. See [Engine tuning is out of scope](#engine-tuning-is-out-of-scope-and-why) and the phase-3 plan's open questions. Revisit at the top of phase 5, with real session history.
- **`Targets` has no phase weighting** (phase 3, step 5) - "decide before it becomes a visible behaviour." Phase 4 makes it visible over HTTP but nothing consumes it until the TUI renders it, so the decision still belongs to phase 5.
- **What a locked-key submission should report.** Decision 3 makes it a silent `201`. A `200` with a per-item "ignored" list is the alternative, and it is more work than it is worth until a client can be wrong by accident rather than on purpose.
- **Whether `GET /progress` should serve derived values** (mean score, unlocked count, a heatmap-ready projection) or stay a raw document passthrough. Phase 5's heatmap is the consumer that will answer this; do not guess now.
- **Session history retention.** The table is append-only and unbounded. Nothing to do for one user; note it before a public deployment (phase 7).
