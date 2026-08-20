# Architecture

> Read [`adr/`](adr/) first if you want to know _why_ - this document covers _what_.

## System overview

```
┌──────────────┐         HTTP / JSON         ┌────────────────────────┐
│  TUI client  │ ──────────────────────────► │      typist server     │
│ (Bubble Tea) │                             │      (Go monolith)     │
└──────────────┘                             └──────────┬─────────────┘
                                                        │
                                                        │ pgx
                                                        ▼
                                              ┌──────────────────┐
                                              │   PostgreSQL 18  │
                                              └──────────────────┘
```

The server is a single binary. The TUI client is a separate binary that consumes the API.

Three executables sharing the domain code:

- `cmd/server/` - REST API, adaptive lesson engine, interacts w/ DB via SQL
- `cmd/tui/` - standalone Bubble Tea client, runs locally, talks to a configured API URL
- `cmd/sshd/` - wish-based SSH server that serves the _same_ Bubble Tea program as `cmd/tui/`, but to remote connections

## Bounded contexts

The server is structured as a modular monolith. Four bounded contexts. Three own persistence; `corpus` is read-only reference data embedded in the binary, not a database-backed context ([ADR 0013](adr/0013-corpus-as-embedded-generated-data.md)):

| Context    | Responsibility                                                            |
| ---------- | ------------------------------------------------------------------------- |
| `auth`     | Registration, login, JWT issue and validation                             |
| `corpus`   | Embedded ngram frequencies and the transition graph that drive generation |
| `progress` | Per-user per-key and per-ngram competency state                           |
| `session`  | Records of completed typing sessions and their results                    |

The `engine` package sits above these contexts, consuming `corpus` and `progress` to produce lessons. It contains the interesting domain logic and is deliberately I/O-free.

## Where the engine runs

`engine` is a pure library, so it runs in two places ([ADR 0014](adr/0014-engine-as-library-state-follows-identity.md)). For identified users (password or SSH key) it runs inside the server, behind the API, with state in Postgres. For anonymous or offline users it runs in-process in the client, against ephemeral or local-file state, never touching the database. The identified path is what the SSH demo exercises end to end; the anonymous path keeps the tool usable with no sign-in wall and without writing rows.

## Layering inside a context

Each context follows a strict three-layer shape:

- **handler.go** - HTTP transport. Parses the request, calls into the service, formats the response. Knows about HTTP; knows nothing about SQL.
- **service.go** - business logic. Orchestrates one or more repositories, manages transactions, enforces invariants. Knows about domain types; knows nothing about HTTP or SQL specifics.
- **repository.go** - persistence. Translates between SQL rows and domain types. Exposes an interface; the concrete implementation uses sqlc-generated code.

Dependencies point downward. Services do not depend on handlers; repositories do not depend on services. Wiring happens in `cmd/server/main.go`.

## Cross-cutting infrastructure

`internal/platform/` holds the load-bearing infrastructure: config loading, the database pool, logging setup, observability. These packages are used by everything but depend on nothing in the bounded contexts.

## Request flow

A typical authenticated request (`GET /api/v1/lessons/next`) flows through:

1. mux dispatches to the registered handler
2. Recovery / RequestID / Logging middleware wrap the handler
3. Auth middleware validates the JWT and injects the user ID into the context
4. The progress handler parses the request and calls `progress.Service.NextLesson(ctx, userID)`
5. The service loads the user's competency state from `progress.Repository`
6. The service calls `engine.NextLesson(state, corpus, now, rand)` - pure
7. The service returns the result up through the handler, which JSON-encodes it

## Write flow

`POST /api/v1/sessions` is the one request that writes across bounded contexts: it records a session _and_ folds the result into competency. The `session` service owns this unit of work, composing the `progress` repository and the pure engine inside a single transaction:

1. begin a transaction
2. load the user's `CompetencyState` via the `progress` repository, selecting the `user_progress` row `FOR UPDATE` (transaction-scoped). The state is a whole-document load-modify-write, so two overlapping submissions for the same user could otherwise lose an update; the row lock serialises them. Contention is per-user (one person typing), so the lock is effectively never contended.
3. `engine.ApplyResult(state, corpus, result, now)` - pure; folds the observations in and applies any unlock or tier advance. The corpus is needed because unlocking asks which key comes next, which only the corpus knows (see [`engine.md`](engine.md#the-two-engine-functions))
4. derive the session's WPM and accuracy from the submitted observations alone - pure; the server does not trust client-computed aggregates, and takes no client-reported duration either, so the summary and the scores come from one input. Summed over keys only: a bigram's time covers two keystrokes, so summing ngrams double-counts
5. insert the `sessions` row via the `session` repository (same transaction)
6. write the updated competency via the `progress` repository (same transaction)
7. commit

The two repositories share one transaction via sqlc's `WithTx`: each is constructed against the same `pgx.Tx`, so the coordinator composes _repositories_, not services, and `session` never depends on the `progress` service. This is where the modular monolith earns its keep - a cross-domain write is one local transaction, with no saga and no eventual-consistency dance ([ADR 0003](adr/0003-modular-monolith.md)).

## What's not in v1

These are deliberate non-goals, not omissions; each the possibility of being revisited.

- Microservices (modular monolith is the right size)
- Message queues (no async work yet)
- Redis cache (no measured need)
- Kubernetes (Docker on a single homelab host is fine)
- GraphQL (REST is the right fit for a TUI consumer)
- OAuth (JWT registration is enough for v1)
- Refresh tokens (v1 issues a single moderate-lived access token - ADR 0015's documented fallback; rotating refresh and the `/auth/refresh` endpoint are deferred and omitted from the API contract until they ship)
- Password reset / email verification (no email infrastructure in v1; revisit when there are real users to lock out of accounts)
- Rate limiting on `/auth/*` (deferred until the public SSH surface ships; the SSH layer has its own per-IP limits in [ADR 0008](adr/0008-ssh-public-key-authentication.md))
