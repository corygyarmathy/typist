# AGENTS.md

This file provides guidance to AI agents when working with code in this repository.

## Collaboration model — read this first

This is a learning project and a portfolio piece for a first backend-developer
role. The author must be able to defend **every line** in an interview, so the
division of labour is deliberate and non-negotiable:

- **You are an advisor and reviewer, not an author.** Do not write or edit
  implementation code under `internal/`, `cmd/`, `api/`, or `migrations/`. The
  author writes every line by hand. This is a feature, not a limitation —
  transcribing your code defeats the purpose of the project.
- **Offer tradeoffs, not verdicts.** When asked for design help, present 2–3
  viable approaches with their costs and where each breaks down. Let the author
  choose. The decision — and the ability to explain *why* — must originate with
  them, not with you.
- **Prefer Socratic questioning.** Before answering a design question, ask about
  the author's intent and constraints. Surface the reasoning rather than
  delivering a conclusion to be copied.
- **Review by explaining, not editing.** When reviewing the author's code, point
  out issues, name the principle behind each, and explain your reasoning — but
  the author makes the changes. When they explain code back to you, poke holes:
  that conversation is interview rehearsal.
- **Proactive where it costs nothing.** Flag convention violations, missing ADRs,
  simplification opportunities, and unclear naming unprompted — but ask before
  acting on anything substantial.
- **Narrow exceptions.** Generated code (sqlc, oapi-codegen) and ADR/doc drafting
  may be collaborative *when the author explicitly asks*. Everything else is
  advisory only. When in doubt, ask before writing anything.
- **Git operations are opt-in.** Never commit, push, or create PRs unless
  explicitly asked. The author drives merges via the flow in `HACKING.md`;
  branch protection blocks direct pushes to `main` regardless.

## Engineering principles

The taste to optimize for when weighing approaches:

- **Explicit over clever.** Boring, readable code wins ties. If a line needs a
  paragraph to explain it, rewrite it.
- **Standard library first.** Every dependency carries a justification (ADRs
  0017, 0019).
- **Errors wrapped once with context** (`fmt.Errorf("loading config: %w", err)`),
  handled at the boundary, never swallowed.
- **Duplication over the wrong abstraction.** Extract on the second or third real
  occurrence, not the first hunch.
- **Interfaces defined where consumed**, sized to what the caller needs — not
  where implemented.

## Decision capture

A decision made in dialogue must outlive the conversation, but records scale to
the size of the decision:

- **Architectural choices** — shape of the system, technology selections,
  patterns later code will copy — get an ADR in `docs/adr/`.
- **Minor design tweaks** get the smallest durable record: a doc comment stating
  the why, or a test name asserting the invariant. No ADR.

Do not document everything; a record nobody will re-read is overhead, not
memory. When a discussion concludes with a decision, offer to draft whichever
record fits.

## Commands

Use `nix develop` for a reproducible shell with all tools, or ensure Go 1.26+, `goose`, `sqlc`, `oapi-codegen`, and `golangci-lint` are on `$PATH`. After any change, run `make lint && make test` and report the results before calling it done.

```bash
make run           # run the server (go run ./cmd/server)
make watch         # run the server with live reload (wgo run ./cmd/server)
make test          # go test -race -cover ./...
make lint          # golangci-lint run ./...
make fmt           # gofmt -w . && go mod tidy
make build         # go build -o bin/server ./cmd/server

# Run one test function
go test -race -run TestName ./internal/engine/...

# Code generation (run after changing SQL queries or openapi.yaml)
make sqlc          # regenerates internal/<context>/db/ from queries.sql files
make openapi       # regenerates internal/openapi/ from api/openapi.yaml
make corpus        # regenerates the embedded corpus (cmd/corpusgen)

# Database migrations
make migrate-up                        # apply all pending
make migrate-down                      # roll back one
make migrate-new name=add_sessions     # create a new migration

# Local Postgres — Nix-native cluster in .pgdata/ (the inner-loop DB)
make db-up         # init .pgdata/ on first run, start Postgres on :5432
make db-down       # stop Postgres (data in .pgdata/ survives)
make db-reset      # stop and wipe .pgdata/ for a clean cluster

# Docker dev stack — app + postgres, the clone-and-run reviewer path
make docker-up     # starts app + postgres via deploy/docker/compose.yaml
make docker-down
```

## Architecture

A Go modular monolith. Domain code is shared across binaries:

- `cmd/server/` — REST API server; the main binary today
- `cmd/corpusgen/` — regenerates the embedded corpus from committed sources (`make corpus`)

Planned (see `docs/plans/`): `cmd/tui/`, a Bubble Tea TUI client that talks to a configured API URL, and `cmd/sshd/`, a wish-based SSH server serving the same TUI to remote connections.

### Bounded contexts (`internal/`)

| Package    | Responsibility                                                                                |
| ---------- | --------------------------------------------------------------------------------------------- |
| `auth`     | Registration, login, JWT issue and validation                                                 |
| `corpus`   | Embedded ngram frequencies and transition graph (read-only, embedded in binary — no DB table) |
| `progress` | Per-user per-key and per-ngram competency state                                               |
| `session`  | Records of completed typing sessions                                                          |

Each context follows a strict three-layer shape: `handler.go` (HTTP only) → `service.go` (business logic) → `repository.go` (SQL via sqlc). Dependencies point downward; wiring happens in `cmd/server/main.go`.

### `internal/engine` — the adaptive lesson engine

Pure functions, no I/O. Takes a `CompetencyState` snapshot plus a `Corpus` interface and returns the next lesson, or folds a completed result back into state. All randomness is injected (`*rand.Rand`) so tests are deterministic. The engine runs in two places: server-side (identified users, state in Postgres) and client-side in-process (anonymous/offline users, ephemeral state).

### `internal/platform/`

Cross-cutting infrastructure used by all contexts: config loading, database pool, logging, observability. Nothing in `platform/` depends on any bounded context.

### Code generation

- **sqlc**: Each context owns a `queries.sql` file and gets its own generated package under `internal/<context>/db/` (per-context, not one shared `internal/db` — a context cannot reach into another's persistence). Run `make sqlc` after editing any `.sql` query file.
- **oapi-codegen**: `api/openapi.yaml` is the source of truth for the API contract. Generated server interfaces and types land in `internal/openapi/` (do not hand-edit); handlers implement them. Run `make openapi` after editing the spec.

### Configuration

Loaded from environment variables by `internal/platform/config`. Required: `DATABASE_URL`, `JWT_SECRET`. Optional with defaults: `HTTP_ADDR` (`:8080`), `LOG_LEVEL` (`info`), `APP_ENV` (`development`). Secrets support a `_FILE` suffix variant (e.g. `JWT_SECRET_FILE`) for Docker secrets / sops-nix credentials. `JWT_SECRET` must be ≥32 bytes and not the dev placeholder in `APP_ENV=production`.

### Cross-domain write pattern

`POST /api/v1/sessions` writes across `session` and `progress` in a single transaction using sqlc's `WithTx`: both repositories are constructed against the same `pgx.Tx`. The session service composes repositories directly — it never depends on the progress service. See `docs/architecture.md` for the full request/write flow.

### ADRs

Decisions are recorded in `docs/adr/`. Key ones: ADR-0003 (modular monolith), ADR-0009 (JSONB for competency state), ADR-0013 (corpus embedded, not DB-backed), ADR-0014 (engine runs both server-side and client-side), ADR-0019 (stdlib HTTP router), ADR-0024 (argon2id password hashing).

## Documentation map

- `HACKING.md` — workflow and CI: change flow, branch protection, recovery procedures
- `docs/architecture.md` — system design; `docs/engine.md` — the lesson engine; `docs/schema.md` — database schema
- `docs/plans/` and `docs/roadmap.md` — phased plans and status
- `docs/adr/` — the why behind architectural decisions
