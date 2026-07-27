# Phase 2 implementation plan - the auth vertical

> A granular, session-to-session build plan for phase 2 of [`../roadmap.md`](../roadmap.md). The roadmap says _what_ phase 2 is and _when_ it's done; this says _in what order we build it_ and _why each decision was made_. It is a living plan - update it as we go.

## Where we are

- Branch to build on: `main` (phase 1 complete - config, logging, pgx pool, goose-at-startup, stdlib mux with Recovery/RequestID/Logging middleware, `/healthz` + `/readyz`, the RFC 7807 `httpx` kit).
- `ai-reference-phase-2` is a **reference implementation only** - a full AI-generated phase 2 we read to learn from, not a branch to merge. We reimplement top-down for understanding, and we deliberately diverge from it in several places (see [Divergences](#divergences-from-the-reference-branch)).
- Migrations already exist and are accepted: `0001_create_users_table`, `0002_create_auth_credentials_table`, `0003_create_user_progress_table`, `0004_create_sessions_table`. **No migration work in phase 2** - the schema is done. We only write queries against it.

## Definition of done (from the roadmap)

> Register → login → call a protected endpoint with the bearer token succeeds; a bad/expired token returns a 401 `problem+json`. Handlers satisfy the `oapi-codegen`-generated interfaces. The `user_progress` row is created in the **same transaction** as the user at registration.

Concretely, phase 2 ships:

- [ ] `POST /auth/register` - creates user + password credential + `user_progress` in one tx, returns a token
- [ ] `POST /auth/login` - verifies credentials, returns a token
- [ ] A single protected endpoint (`GET /progress`, minimal) to prove the middleware end-to-end
- [ ] argon2id password hashing (+ the two production defenses, below)
- [ ] HS256 JWT issue + validate (single ~24h access token; no refresh - see decisions)
- [ ] Bearer-auth middleware injecting the user ID into request context
- [ ] Generated types/interfaces from `api/openapi.yaml`, implemented by our handlers
- [ ] Tests at every layer + one register→login→protected e2e

## Decisions (locked in for phase 2)

Each was reviewed against "well-constructed and idiomatic over clever." Three were chosen by Cory on 2026-07-03; the rest follow from the roadmap/ADRs.

### 1. Adopt oapi-codegen now (spec-first) - _chosen_

We generate request/response types **and** a server interface from `api/openapi.yaml`, and our handlers implement it. Rationale: the roadmap's done-condition and ADR 0004 (REST) already commit to spec-first; the spec becomes the single source of truth for the contract; and "wiring generated interfaces" is a mainstream Go-shop skill worth demonstrating. Cost we accept: generated code is code we don't hand-write - so the _understanding_ target shifts to "understand how the generator is configured, what it emits, and how we satisfy it," which we'll cover explicitly.

- Keep `internal/platform/httpx` as the platform kit. **Do not** rename it to `internal/api` (the reference branch did - we reject that; see divergences).
- Generate into one package for the whole spec (proposal: `internal/openapi`). oapi-codegen emits **one** `ServerInterface` covering _all_ operations, plus the `BearerAuthScopes` marker.
- Because handlers live per-context (`internal/auth/handler.go`, `internal/progress/handler.go`) but the generated interface is monolithic, the **composition root** (`cmd/server`) defines a small aggregate type that embeds/delegates to each context's handler and thereby satisfies `ServerInterface`. This keeps the bounded-context boundary while satisfying one generated interface.
  → This aggregation pattern is worth a short ADR (see [Documentation impact](#documentation--tooling-impact)).

### 2. Injected, transaction-bound progress initializer for the cross-context write - _chosen_

Registration writes `users` + `auth_credentials` (auth's tables) **and** `user_progress` (the progress context's table) in one transaction. Rather than have auth's SQL reach into another context's table, the auth service depends on a small `ProgressInitializer` interface and a factory `func(pgx.Tx) ProgressInitializer` injected at the composition root. Auth never imports progress's generated `db` package; the boundary stays real, and we get to demonstrate DI + transaction orchestration in the service layer (never in handlers or repositories).

### 3. argon2id: core + the two production defenses - _chosen_

- **Core:** derive with `argon2.IDKey`, store in **PHC string format** (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`) so parameters + salt travel with the digest and old hashes stay verifiable after a parameter bump; verify by re-deriving with the _stored_ params and a **constant-time** compare (`crypto/subtle`).
- **Defense A - dummy-hash timing equalizer.** On login, if the email is unknown (or malformed), still run a verify against a fixed dummy hash so an unregistered email costs the same wall-clock time as a registered one. Without it, login is an email-enumeration oracle.
- **Defense B - memory semaphore.** Bound concurrent argon2 operations (each costs ~64 MiB) with a buffered channel sized to CPU budget, so a burst of register/login requests queues (backpressure) instead of OOMing a small host. (Rate-limiting on `/auth/*` remains separately deferred per ADR 0008.)

### 4. Single moderate-lived access token, no refresh (roadmap scope)

ADR 0015 (rotating refresh tokens) is **Proposed**, and the roadmap's scope explicitly uses that ADR's documented fallback for v1: one HS256 token, ~24h, no `refresh_tokens` table, and `/auth/refresh` **omitted** from the contract until refresh actually ships. The user ID travels in the standard `sub` claim. `Validate` pins the algorithm to HS256 (rejecting an `alg: none` downgrade) and requires `exp`.

## Divergences from the reference branch

Read the reference for shape, but **do not copy these**:

| Reference did                                           | We do                                                        | Why                                                                                          |
| ------------------------------------------------------- | ------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| Renamed `httpx` → `internal/api`, deleted the httpx kit | Keep `httpx`; **add** `WithUserID`/`UserIDFromContext` to it | The rename is churn with no benefit; phase 1 chose `httpx` deliberately                      |
| Column `kind` in `auth_credentials`                     | Column **`cred_kind`**                                       | `main`'s accepted migration uses `cred_kind` (commit 2d68f0a) - the reference regressed this |
| Renamed migrations (`0002_auth_credentials.sql` etc.)   | Leave migrations as-is                                       | They're accepted; renaming an applied migration is a footgun                                 |
| Module `github.com/corygyarmathy/typing-trainer`        | Module `github.com/corygyarmathy/typist`                     | The reference used the wrong module path                                                     |

## Documentation & tooling impact

- **`go.mod` additions** (the file's header comment already predicts these): `golang.org/x/crypto/argon2`, `github.com/golang-jwt/jwt/v5`, `github.com/oapi-codegen/runtime`.
- **`api/oapi-codegen.yaml`** - new. The `Makefile` `openapi` target already points at it; the file itself doesn't exist yet. Configure: target package (`internal/openapi`), `std-http-server` generation, models, embedded-spec off (or on, our call), and the strict-server option if we want typed request/response wrappers.
- **`api/openapi.yaml`** - add: `securitySchemes.bearerAuth` (http/bearer/JWT), a default `security: [bearerAuth]` with `security: []` overrides on the public ops, the `/auth/register` and `/auth/login` paths, `GET /progress` (protected), and schemas `RegisterRequest`, `LoginRequest`, `TokenResponse` (+ reuse the existing `Problem`).
- **`internal/platform/config/config.go`** - add `JWTAccessTTL time.Duration` (default 24h; env `JWT_ACCESS_TTL`). `JWTSecret` + its production guard already exist - reuse them.
- **`Makefile`** - targets already exist (`sqlc`, `openapi`); no change expected.
- **ADRs to propose** (Cory's working style = decisions become ADRs):
  - _argon2id password hashing_ - the algorithm choice, the parameters, PHC storage, and the two defenses. (No existing ADR covers password hashing.)
  - _oapi-codegen server generation + composition-root aggregation_ - the codegen tooling choice and the monolithic-interface-vs-per-context-handlers reconciliation. (Optional; may fold into ADR 0004.)

## The build order (outside → in)

The rule: **build each function only when the layer above it needs it.** Expect backtracking - that's the point. Each step below names what we write, why, and what it forces us to pull in next. We do the **register** vertical fully first (it exercises the hardest path: cross-context tx), then login reuses most of it, then the middleware + protected endpoint close the loop.

**Concepts Cory flagged as unclear - we slow down and teach these when we reach them:** JWT issue/validate (step 7) and the auth middleware (step 8). sqlc queries (rusty) get a refresher at step 5.

### Stage 0 - Contract & tooling (the outermost layer)

1. **Write the contract in `api/openapi.yaml`.** Add the security scheme, the two `/auth/*` paths, `GET /progress`, and the schemas. This is the outside edge - everything below serves it.
2. **Create `api/oapi-codegen.yaml` and generate.** `make openapi`. Inspect the emitted `ServerInterface`, the request/response types, and `BearerAuthScopes`. Understand what we now have to implement. Add the runtime dep; `make fmt` / `go mod tidy`.

### Stage 1 - Route the skeleton (prove it compiles & serves)

3. **Composition-root aggregate in `cmd/server`.** Define the type that will satisfy the generated `ServerInterface` by delegating to per-context handlers. Wire the generated router onto the existing mux alongside `/healthz`/`/readyz`. Stub `RegisterUser`/`LoginUser`/`GetProgress` to return 501 so it compiles and routes. **Checkpoint:** `curl` a stub route → 501 through the real generated wiring.

### Stage 2 - The register vertical (the hard path first)

4. **`auth/handler.go` - `RegisterUser`.** Thin: unmarshal the generated request type, call `service.Register(ctx, email, password)`, shape the `TokenResponse` via `httpx`. This forces `Service.Register` to exist → pull it in.
5. **`auth/service.go` - `Register` (skeleton).** Orchestrates: `normalizeEmail` (inline validation) → duplicate pre-check → hash → **begin tx** → create user → create credential → seed progress → commit → issue token. Each call below doesn't exist yet; pull them in in this order:
   - **`auth/queries.sql` + `make sqlc`** (the rusty-skill refresher). Write `CreateUser`, `PasswordCredentialExists`, `CreateCredential`, `GetCredentialByIdentifier`. **Use `cred_kind`**, not `kind`. Note nullable columns → `emit_pointers_for_null_types` gives `*string` for `display_name`/`secret`. Inspect the generated `db` package.
   - **`auth/repository.go`.** `Repository` interface + `pgxRepository` over `db.DBTX` (satisfied by both `*pgxpool.Pool` and `pgx.Tx` - this is what lets registration share one tx). Methods: `CreateUser`, `EmailRegistered`, `CreatePasswordCredential` (map Postgres unique-violation `23505` → `ErrEmailTaken`), `FindPasswordCredential` (for login later). Return domain types, not rows.
   - **`auth/password.go`.** Start minimal: `hashPassword` (PHC) + `verifyPassword` (constant-time). Get register working end-to-end first, _then_ add Defense A (dummy hash) and Defense B (semaphore) with their justifications - incremental so each line is understood.
   - **`ProgressInitializer` + factory.** Define the interface in `auth/service.go` (consumer owns it). In `internal/progress`: add `CreateUserProgress` query (`make sqlc`), a repo method, and a `CreateInitial(ctx, userID)` that inserts the initial competency JSON. **Seam:** the engine (phase 3) doesn't exist, so define the initial competency doc as a small placeholder constant/func now and leave a `TODO(phase-3)` to replace it with `engine.InitialCompetency()`.
   - **`auth/jwt.go` - `Authenticator.Issue`** (see step 7; `Register` needs `Issue` to return a token).
   - **`auth/models.go`.** `User`, `Token`, and the error sentinels (`ErrEmailTaken`, `ErrInvalidEmail`, `ErrPasswordTooShort`, `ErrInvalidCredentials`).
6. **Map service errors → HTTP status in the handler** (or a small `httpx` helper): `ErrEmailTaken` → 409, validation sentinels → 400, unknown → 500, all as `problem+json`. **Checkpoint:** register a user → 200 + token; register again → 409; verify all three rows exist in one tx (rollback on any failure).

### Stage 3 - Login (reuses most of stage 2)

7. **`auth/jwt.go` - `Authenticator` (full) + teach JWT.** `Issue` (HS256, `sub`/`iat`/`nbf`/`exp`, inject `now` for deterministic tests), `Validate` (`WithValidMethods(["HS256"])`, `WithExpirationRequired`, parse `sub` → `uuid.UUID`), `TTLSeconds`. Wire `NewAuthenticator` in the composition root from config (`JWTSecret`, `JWTAccessTTL`). **This is a teaching step** - Cory flagged JWT as unclear.
8. **`auth/service.go` - `Login` + `auth/handler.go` - `LoginUser`.** Look up credential; on unknown-email/malformed-email run the dummy-hash verify (Defense A); constant-time verify; issue token. Return `ErrInvalidCredentials` for _both_ unknown email and wrong password (no enumeration). **Checkpoint:** login with good creds → token; wrong password and unknown email → identical 401 + similar timing.

### Stage 4 - Middleware & the protected endpoint (close the loop)

9. **`httpx/context.go` - add `WithUserID` / `UserIDFromContext`.** The `userIDKey` const already exists; add the setter/getter (`uuid.UUID`).
10. **`auth/middleware.go` - `RequireAuth` + teach middleware.** Define a small `Validator` interface (consumer-owned, so it's testable with a fake). The middleware reads the generated `BearerAuthScopes` context marker to tell protected ops from public ones, extracts the bearer token (case-insensitive scheme per RFC 7235), validates it, and injects the user ID - or writes a 401 `problem+json`. Wire it as the oapi middleware so it runs after the generated wrapper sets the marker but before the handler. **This is a teaching step** - Cory flagged middleware as unclear.
11. **`progress/handler.go` - minimal `GetProgress` (protected).** Read the user ID from context, load the competency via a `GetUserProgress` query/repo method, return it. This is the phase-2 proof of the middleware; the full progress vertical is still phase 4. **Checkpoint (the roadmap's DoD):** register → login → `GET /progress` with the bearer token → 200; with a bad/expired/missing token → 401 `problem+json`.

## Testing strategy

- **Unit (fast, no DB):**
  - `password_test.go` - hash≠plain, verify round-trips, wrong password fails, tampered PHC string rejected, dummy-hash path returns false without error.
  - `jwt_test.go` - issue→validate round-trip; expired token rejected (inject `now`); `alg: none` and HS256-with-wrong-secret rejected; non-UUID `sub` rejected.
  - `middleware_test.go` - public op passes through; missing/garbage/expired token → 401; valid token injects the user ID. Uses a fake `Validator`.
  - `service_test.go` - `Register`/`Login` against a fake `Repository` + fake `ProgressInitializer`: duplicate-email → `ErrEmailTaken`, short password → `ErrPasswordTooShort`, wrong password → `ErrInvalidCredentials`.
- **Handler:** table-driven, decode the generated types, assert status + `problem+json` shape.
- **Integration/e2e (needs Postgres):** one test that runs register → login → `GET /progress` against a real (or testcontainers) DB, and asserts the registration tx is atomic (force the progress insert to fail → assert no user/credential rows remain).

## Open questions / seams for later

- **Initial competency doc.** Placeholder in phase 2; replaced by the engine's canonical initial state in phase 3 (`TODO(phase-3)` at the progress initializer).
- **oapi-codegen output package name** (`internal/openapi` vs `internal/api`) - decide at Stage 0 step 2; trivial to change while nothing imports it.
- **strict-server vs plain server interface** - decide when we see what the generator emits (Stage 0); strict gives typed request/response wrappers at the cost of more generated surface.
- **`/auth/refresh`** stays omitted until ADR 0015 is accepted and refresh actually ships (phase 9).
