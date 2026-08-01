# Roadmap: the v1 vertical slice

> This is a living plan, not a contract. It sequences the build into a thin, end-to-end vertical slice that exercises every layer, then iterates. The [`adr/`](adr/) records cover _why_; [`architecture.md`](architecture.md) covers _what_; this covers _in what order_.

## Goal

The thinnest end-to-end build that exercises every layer of the architecture and is genuinely usable. Definition of done for the slice:

> A user runs `docker compose up`, registers with email/password in the standalone TUI, is served a generated lesson, types it under force-correction, submits it, watches the engine fold the result into competency (scores move, a key unlocks), and sees the updated heatmap - all through the real REST API, with state in Postgres.

## Scope

The slice is **password auth + bigrams + Docker-local**. Deliberately deferred (each already recorded as an ADR):

- SSH demo surface - [ADR 0007](adr/0007-ssh-as-a-demo-surface.md), [0008](adr/0008-ssh-public-key-authentication.md), and the token-exchange in [0010](adr/0010-unified-identity-and-jwt.md)/[0016](adr/0016-asymmetric-jwt-signing.md)
- Anonymous / offline in-process engine - [ADR 0014](adr/0014-engine-as-library-state-follows-identity.md)
- Rotating refresh tokens - [ADR 0015](adr/0015-access-and-refresh-tokens.md) (v1 uses that ADR's documented fallback: a single moderate-lived access token; `/auth/refresh` is omitted from the API contract until refresh ships)
- Trigrams (bigrams first; the generator emits both, the engine chooses)
- Production NixOS deployment - [ADR 0006](adr/0006-docker-and-nix.md), [0011](adr/0011-configuration-and-secrets-management.md)

## Guiding principle

Stay **vertical** at every step. Get a thin request flowing through `mux → service → sqlc → Postgres → TUI` before deepening any one layer. This front-loads integration risk - auth and the cross-context transactional write, where bugs actually hide - instead of building a polished engine in isolation and discovering the wiring late.

## Phases

Phase numbers align with the `phase-N` markers already in the code TODOs.

### Phase 1 - Platform spine & walking skeleton

Config loader (`Secret` type, `_FILE` support, production guard), structured logging, pgx pool, goose-at-startup under an advisory lock, mux with Recovery / RequestID / Logging middleware, `/healthz` + `/readyz`, the RFC 7807 error helper. `users` migration only.

**Done when:** `docker compose up` brings up app + Postgres, migrations apply, `/readyz` returns 200 (proves the DB path) and `/healthz` returns 200.

### Phase 2 - Auth vertical

`auth_credentials` migration, `internal/auth` sqlc queries, argon2id hashing, JWT issue + validate (HS256, single ~24h token), auth middleware injecting the user ID, `POST /auth/register` + `POST /auth/login`. The `user_progress` row is created in the **same transaction** as the user at registration.

**Done when:** register → login → call a protected endpoint with the bearer token succeeds; a bad/expired token returns a 401 `problem+json`. Handlers satisfy the `oapi-codegen`-generated interfaces.

### Phase 3 - Corpus + engine (the brain), bigrams only

Pure and parallelisable with phases 1–2. `cmd/corpusgen` → embedded artifact + frequency-validation test (assert against the [Norvig/Mayzner](https://norvig.com/mayzner.html) reference). Engine: `CompetencyState` / `ItemScore` / `Observation` / `Lesson`, scoring (accuracy-weighted EMA), recency decay, key unlocking, generation (weighted walk over the bigram transition graph), `ApplyResult` (including the target-WPM raise). Keep ngram _scoring_ in; ngram-tier progression can be thin here and deepen later. Full spec: [`engine.md`](engine.md).

**Done when:** unit + property tests pass (e.g. `testing/quick`: a generated lesson contains only unlocked keys) **and** the simulated-user harness drives a "good" and a "struggling" learner through the alphabet in a bounded number of lessons.

> The simulated-user harness is the strongest single portfolio artifact here - it demonstrates the design was validated, not just shipped. Lean on it.

### Phase 4 - Progress & sessions vertical (closes the server loop)

`sessions` migration, per-context sqlc for `progress` / `session`, repositories, services. Wire `GET /lessons/next` (pure read), `GET /progress`, `GET /sessions` (keyset cursor), and the cross-context `POST /sessions` - the transactional write: `SELECT … FOR UPDATE` on `user_progress`, `engine.ApplyResult`, server-derived WPM/accuracy, both writes, commit (see the write flow in [`architecture.md`](architecture.md)).

**Done when:** a scripted loop (register → next → submit → progress) shows competency changing across calls; the FOR-UPDATE write is covered by a concurrency test.

### Phase 5 - Standalone TUI client

Bubble Tea: API client layer (token storage in `$XDG_STATE_HOME`, bearer attach), the typing screen implementing **force-correction input and the per-item attribution rules** from the engine doc, a results screen, and the progress / keyboard heatmap. `cmd/tui` only.

**Done when:** the full loop is playable against the local server, and the observations the client submits match the engine's attribution spec.

### Phase 6 - Package & demo locally

Multi-stage Dockerfile, compose with seeded dev placeholders, verify the README quickstart verbatim on a clean checkout, wire the existing Prometheus metrics, record a short asciinema for the README.

**Done when:** a fresh clone → `docker compose up` → reviewer plays the loop with zero extra steps. **This is the shippable vertical slice.**

## Beyond the slice (iterate)

- **Phase 7** - Production NixOS deploy (homelab, domain, TLS).
- **Phase 8** - SSH surface: wish, pubkey auth, anonymous fallback, the internal token-exchange endpoint + service credential ([ADR 0008](adr/0008-ssh-public-key-authentication.md), [0016](adr/0016-asymmetric-jwt-signing.md)).
- **Phase 9** - Refresh tokens ([ADR 0015](adr/0015-access-and-refresh-tokens.md)), the anonymous / offline local-file engine ([ADR 0014](adr/0014-engine-as-library-state-follows-identity.md)), trigrams.
- **Phase 10** - Demo polish: asciinema / gif, README, metrics dashboard.
- **Spec-conformance testing** (once the phase-4 surface is real) - validate live request/response bodies against `api/openapi.yaml` at runtime, so behaviour cannot drift from the contract the way the generated _types_ already cannot (enforced by the `codegen` CI job). Prefer the in-ecosystem path: enable `embedded-spec` in `api/oapi-codegen.yaml` to get `GetSwagger()` (a `kin-openapi` document), then assert conformance inside the existing `go test` - no new language or CI service. An external spec-fuzzer (Schemathesis / Dredd) is the heavier alternative if property-based negative testing is wanted later.
- **Spec-driven security & request validation** - let the spec's own `security` requirements decide which operations need a credential, via [`nethttp-middleware`](https://github.com/oapi-codegen/nethttp-middleware) over `kin-openapi`'s `openapi3filter`. Shares the `embedded-spec` enabling step with the bullet above, so the two are worth doing together. Motivation: `oapi-codegen` v2.8.0 ([#2440](https://github.com/oapi-codegen/oapi-codegen/pull/2440)) removed the generated `BearerAuthScopes` context marker that the auth middleware used to gate on, because a flattened scope list cannot express alternative (OR), combined (AND), or anonymous security requirements. v1 replaces it with an explicit default-deny list of public routes in `cmd/server/router.go`; that list is the drift this bullet removes. Costs, all understood and deliberately deferred:
  - `openapi3filter.AuthenticationFunc` returns only an `error` and cannot write to the downstream request context, so a thin `RequireAuth` must survive to inject the user ID. The change removes the route list, not the middleware.
  - `format:` is **not** validated unless explicitly enabled, and `email` is not a built-in - so `ErrInvalidEmail` stays service-side regardless. Draw the line at: spec owns _shape_ (types, required, lengths, enums), service owns _meaning_.
  - `minLength` / `maxLength` on passwords would move to the spec, making `auth.MinPasswordLen` a derived value rather than the source of truth. Decide who owns it before starting.
  - Validator errors need an `ErrorHandler` mapping into `problem+json`, or error bodies become inconsistent with [ADR 0019](adr/0019-stdlib-http-router.md).
  - `kin-openapi` is pre-v1 and breaks between minor versions; this adds it as a direct dependency ([ADR 0017](adr/0017-hybrid-dependency-management.md)).

## Sequencing notes

- **Phase 3 can run in parallel** with 1–2 - it imports nothing from them. Do it alongside the spine work and converge at phase 4.
- **Risk-ordering:** auth (2) and the transactional write (4) are where integration bugs hide; the plan puts a working spine under them early rather than last.
