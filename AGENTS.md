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
- **Narrow exceptions.** Generated code (sqlc, oapi-codegen) and ADR/doc drafting
  may be collaborative *when the author explicitly asks*. Everything else is
  advisory only. When in doubt, ask before writing anything.

## Commands

Use `nix develop` for a reproducible shell with all tools, or ensure Go 1.26+, `goose`, `sqlc`, `oapi-codegen`, and `golangci-lint` are on `$PATH`.

```bash
make run           # run the server (go run ./cmd/server)
make watch         # run the server with live reload (wgo run ./cmd/server)
make test          # go test -race -cover ./...
make lint          # golangci-lint run ./...
make fmt           # gofmt -w . && go mod tidy
make build         # go build -o bin/server ./cmd/server

# Run a single test package
go test -run TestName ./internal/adaptive/...

# Code generation (run after changing SQL queries or openapi.yaml)
make sqlc          # regenerates internal/db/ from queries.sql files
make openapi       # regenerates server interfaces from api/openapi.yaml

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

A Go modular monolith. Three binaries share the domain code:

- `cmd/server/` — REST API server; the main binary
- `cmd/tui/` — Bubble Tea TUI client that talks to a configured API URL
- `cmd/sshd/` — wish-based SSH server serving the same TUI to remote connections

### Bounded contexts (`internal/`)

| Package    | Responsibility                                                                                |
| ---------- | --------------------------------------------------------------------------------------------- |
| `auth`     | Registration, login, JWT issue and validation                                                 |
| `corpus`   | Embedded ngram frequencies and transition graph (read-only, embedded in binary — no DB table) |
| `progress` | Per-user per-key and per-ngram competency state                                               |
| `session`  | Records of completed typing sessions                                                          |

Each context follows a strict three-layer shape: `handler.go` (HTTP only) → `service.go` (business logic) → `repository.go` (SQL via sqlc). Dependencies point downward; wiring happens in `cmd/server/main.go`.

### `internal/adaptive` — the engine

Pure functions, no I/O. Takes a `CompetencyState` snapshot plus a `Corpus` interface and returns the next lesson, or folds a completed result back into state. All randomness is injected (`*rand.Rand`) so tests are deterministic. The engine runs in two places: server-side (identified users, state in Postgres) and client-side in-process (anonymous/offline users, ephemeral state).

### `internal/platform/`

Cross-cutting infrastructure used by all contexts: config loading, database pool, logging, observability. Nothing in `platform/` depends on any bounded context.

### Code generation

- **sqlc**: Each context owns a `queries.sql` file and gets its own generated package under `internal/<context>/db/` (per-context, not one shared `internal/db` — a context cannot reach into another's persistence). Run `make sqlc` after editing any `.sql` query file.
- **oapi-codegen**: `api/openapi.yaml` is the source of truth for the API contract. Server interfaces are generated from it; handlers implement them. Run `make openapi` after editing the spec.

### Configuration

Loaded from environment variables by `internal/platform/config`. Required: `DATABASE_URL`, `JWT_SECRET`. Optional with defaults: `HTTP_ADDR` (`:8080`), `LOG_LEVEL` (`info`), `APP_ENV` (`development`). Secrets support a `_FILE` suffix variant (e.g. `JWT_SECRET_FILE`) for Docker secrets / sops-nix credentials. `JWT_SECRET` must be ≥32 bytes and not the dev placeholder in `APP_ENV=production`.

### Cross-domain write pattern

`POST /api/v1/sessions` writes across `session` and `progress` in a single transaction using sqlc's `WithTx`: both repositories are constructed against the same `pgx.Tx`. The session service composes repositories directly — it never depends on the progress service. See `docs/architecture.md` for the full request/write flow.

### ADRs

Decisions are recorded in `docs/adr/`. Key ones: ADR-0003 (modular monolith), ADR-0009 (JSONB for competency state), ADR-0013 (corpus embedded, not DB-backed), ADR-0014 (engine runs both server-side and client-side).
