# ADR 0021: Generate the HTTP server contract with oapi-codegen

- **Status:** Accepted
- **Date:** 2026-07-06
- **Related Artefacts:**
  - Implements: `api/oapi-codegen.yaml`, `internal/openapi/openapi.gen.go`, `.github/workflows/ci.yml` (`codegen` job)
  - Constrains: `cmd/server` (aggregate handler), `internal/auth/handler.go`, `internal/progress/handler.go`
  - Constrained by: [ADR 0004](/docs/adr/0004-rest-over-grpc.md) (REST + OpenAPI), [ADR 0019](/docs/adr/0019-stdlib-http-router.md) (stdlib router)
  - Relates to: [ADR 0003](/docs/adr/0003-modular-monolith.md) (modular monolith), [ADR 0005](/docs/adr/0005-sqlc-and-goose.md) (committed codegen precedent), [ADR 0017](/docs/adr/0017-hybrid-dependency-management.md) (tool pinning)

## Context

The API contract lives in `api/openapi.yaml`. [ADR 0004](/docs/adr/0004-rest-over-grpc.md) committed to REST with an OpenAPI document as the contract and a spec-first workflow. That leaves one question open: how the Go server relates to the spec. The request/response types and the server signature can be hand-written to match the document, or generated from it. Hand-writing duplicates the contract and lets the implementation drift from it silently - which defeats the purpose of having a single source of truth.

Two existing decisions constrain the answer. [ADR 0019](/docs/adr/0019-stdlib-http-router.md) chose `net/http`'s `ServeMux` and rejected a framework router, so whatever generates the server must target the standard library, not `chi`/`gin`/`echo`. And [ADR 0003](/docs/adr/0003-modular-monolith.md)'s modular monolith puts handlers in per-context packages (`auth`, `progress`), yet a spec-wide generator emits a **single** interface covering every operation in the document - so a monolithic generated interface has to be reconciled with bounded-context handlers without letting the contexts bleed into one another.

## Decision

Use `oapi-codegen` to generate Go from `api/openapi.yaml`, configured by `api/oapi-codegen.yaml`, into one package `internal/openapi` (`openapi.gen.go`). Specifically:

- Generate `models` + `std-http-server`. `std-http-server` mounts onto the stdlib `ServeMux` ([ADR 0019](/docs/adr/0019-stdlib-http-router.md)); no router dependency is introduced.
- Pin the generator version and commit the generated file, following the committed-codegen practice established for sqlc/goose in [ADR 0005](/docs/adr/0005-sqlc-and-goose.md) and the tool-pinning approach of [ADR 0017](/docs/adr/0017-hybrid-dependency-management.md). Regeneration is then reproducible and each change shows up as a reviewable diff rather than a silent, machine-dependent output.
- Keep `internal/platform/httpx` as a distinct package. The generated package holds the contract types and the server interface; `httpx` holds the RFC 7807 problem-details and error plumbing. These are different concerns - generating the contract does not motivate merging or renaming the platform kit.
- Generate `strict-server`, layered on `std-http-server`. The strict layer gives each operation a typed signature - `LoginUser(ctx, LoginUserRequestObject) (LoginUserResponseObject, error)` - and one generated response type per declared status code, so returning an undeclared status or a mismatched body is a compile error, not a runtime surprise. The generated layer also decodes and content-type-checks request bodies, removing hand-written decoding.
- Route RFC 7807 problems through the strict error hooks, not the default handler. `NewStrictHandler`'s default renders both request and response-side errors with `http.Error` (plain text), which would bypass `httpx`'s `problem+json` plumbing. So the composition root builds the handler with `NewStrictHandlerWithOptions` and points both `RequestErrorHandlerFunc` (undecodable/malformed requests) and `ResponseErrorHandlerFunc` (a handler returning `error`, or an unexpected response type) at `httpx.WriteProblem`. Error responses that the spec declares are returned as their generated typed values (e.g. `RegisterUser409ApplicationProblemPlusJSONResponse`, itself typed as `Problem`); the hooks cover only the unexpected paths. The problem-details renderer thus stays a single choke point rather than an ad-hoc call in each handler.
- ~~The generator emits one `ServerInterface` for all operations, plus a per-operation security marker (`BearerAuthScopes`) derived from each operation's `security` requirement. The auth middleware reads this generated marker rather than a hand-maintained list of protected routes.~~ **Superseded** - the marker was removed in `oapi-codegen` v2.8.0; see [Update (2026-08-01)](#update-2026-08-01-the-generated-security-marker-is-gone). The generator still emits one `ServerInterface` for all operations.
- Reconcile the monolithic interface with per-context handlers via a **composition-root aggregate** in `cmd/server`: a small type that embeds/delegates to each context's handler and thereby satisfies `StrictServerInterface`. `NewStrictHandlerWithOptions` then adapts that aggregate into the `ServerInterface` mounted on the mux. Contexts never import each other, and only the composition root assembles the whole interface.

## Consequences

**Positive**

- The request/response types and the server contract are generated from one source, so the implementation cannot silently drift from `api/openapi.yaml`.
- `std-http-server` preserves [ADR 0019](/docs/adr/0019-stdlib-http-router.md)'s zero-router-dependency stance; nothing new to vet or upgrade.
- A pinned generator plus a committed output means regeneration is reproducible across machines and every change is a reviewable diff, consistent with the codegen discipline of [ADR 0005](/docs/adr/0005-sqlc-and-goose.md).
- ~~The security marker is emitted mechanically from the spec's `security` blocks, so "is this route protected?" is answered by generated code, not by a list a developer must remember to update.~~ **Superseded** - see [Update (2026-08-01)](#update-2026-08-01-the-generated-security-marker-is-gone). The question is now answered by a list in `cmd/server`, held to the spec by a test rather than by the generator.
- Bounded contexts ([ADR 0003](/docs/adr/0003-modular-monolith.md)) survive intact: handlers stay in their own packages, and the aggregate is the single, explicit place the full interface is composed.
- Response shape and status are checked at compile time: a handler can only return one of the generated response types for its operation, so an undeclared status or a mismatched body fails to build rather than reaching a client. Request-body decoding is generated, not hand-written.

**Negative**

- Generated code is code we do not hand-write; the understanding target shifts to "how the generator is configured and what it emits." Mitigated by keeping `oapi-codegen.yaml` minimal and reviewing the generated output on each change.
- The single `ServerInterface` is a mild coupling point: adding an operation to any one context changes a shared interface, and the composition-root aggregate must be updated in lockstep.
- `/healthz` and `/readyz` are declared in the spec, so they fall under the generated interface even though they are registered directly on the mux; wiring must avoid double-registering those routes (resolved when the aggregate is wired).
- The generated strict layer auto-decodes request bodies and enforces content types; escaping that (streaming, multipart, raw-body access) means stepping outside the typed signature. A non-issue for this JSON API, but a real constraint if a future endpoint needs it.
- The strict handler's default error rendering is plain-text `http.Error`, so the RFC 7807 contract depends on the aggregate being built with `NewStrictHandlerWithOptions` and both error hooks wired to `httpx.WriteProblem` (see Decision). Building it with the bare `NewStrictHandler` is a quiet trap that would emit plain-text errors instead of `problem+json`.

## Enforcing the reproducibility claim (CI)

The Decision commits to a pinned generator and a committed output "so that regeneration is reproducible and each change is a reviewable diff." That property is only real if something checks it. The `codegen` job in `.github/workflows/ci.yml` regenerates through `nix develop` - which supplies the flake-pinned `oapi-codegen` ([ADR 0017](/docs/adr/0017-hybrid-dependency-management.md)) - and fails on a non-empty `git diff` of `internal/openapi/openapi.gen.go`. This catches the two ways the committed file can stop matching the spec: a stale output (the spec changed but `make openapi` was not re-run) and a hand-edit to generated code. It is drift/hygiene enforcement, not a security control.

Spec-trust is deliberately out of scope. `oapi-codegen` at the pinned version does not validate its input and is permissive of malformed specs; the known RCE-style exploits against it rely on feeding a **remote or untrusted** specification into the generator. That threat does not apply here: `api/openapi.yaml` is first-party, committed, and reviewed, and nothing fetches a spec at build time (the Docker build only runs `go build`). Native spec validation lands in `oapi-codegen` v2.8.0, which is the correct place for it; we adopt it on upgrade rather than bolting a separate validator or an `init()`-emission tripwire onto the build - disproportionate machinery for a single-author, first-party spec.

## Update (2026-08-01): the generated security marker is gone

**Supersedes** the `BearerAuthScopes` bullet in Decision and the security-marker bullet under Consequences → Positive. Both described a mechanism that no longer exists; the rest of this ADR stands.

`oapi-codegen` v2.8.0 stopped emitting the per-operation security marker ([#2440](https://github.com/oapi-codegen/oapi-codegen/pull/2440)). Upstream's reasoning is that a flattened `[]string` of scopes on a context key cannot represent alternative (OR), combined (AND), or anonymous (`{}`) security requirements, so the signal was lossy by construction. A `compatibility.enable-auth-scopes-on-context` flag restores it, defaulting off.

That flaw does not bite this spec - one scheme, no scopes - but the flag exists as a migration path for a mechanism upstream intends to remove, so we migrate rather than pin to it.

The deeper point is that the marker was never authentication data. It was the generated wrapper answering *"does this operation carry a `security` block?"* - a routing question, smuggled through the request context. `http.Request.Pattern` ([Go 1.23+](https://pkg.go.dev/net/http#Request)) answers the same question from the standard library, holding the exact `ServeMux` pattern that matched (`"GET /api/v1/progress"`).

**Decided:**

- `auth.RequireAuth` gates on `r.Pattern` against a set of public routes passed in by the caller. `internal/auth` no longer imports `internal/openapi`, so the auth middleware stops depending on generated code.
- The set lives in `cmd/server` (`publicRoutes`), alongside the route composition it belongs to, and lists **public** routes rather than protected ones. The default is therefore deny: a route added to the spec but omitted from the set fails closed with a 401 rather than serving unguarded. An empty `r.Pattern` - a request no mux routed - is likewise treated as protected.
- `TestRouter_SpecDrift` parses `api/openapi.yaml` and asserts `publicRoutes` equals the set of operations declaring `security: []`. This is what keeps the hand-maintained list honest, and it is the reason the list is acceptable at all: the spec remains the source of truth, and disagreement is a build failure rather than a silent change to which endpoints need a token. It parses with `gopkg.in/yaml.v3` rather than `kin-openapi`, keeping that dependency out of the module until the alternative below is taken up.

**Cost, stated plainly:** the public/protected split is now expressed twice - once in the spec, once in Go - where it used to be derived. The drift test converts that duplication from a silent correctness risk into a mechanical one. It also does not scale to scopes: if a second security scheme or a scoped requirement is introduced, a `map[string]bool` cannot express it and this decision must be revisited.

### The alternative, deferred

Upstream's own recommendation is request-validation middleware ([`nethttp-middleware`](https://github.com/oapi-codegen/nethttp-middleware) over `kin-openapi`'s `openapi3filter`), which evaluates the spec's `security` requirements directly and removes the duplication entirely. It is recorded as post-v1 work in [`roadmap.md`](/docs/roadmap.md), where it shares the `embedded-spec` enabling step with spec-conformance testing. Deferred because it does not deliver a clean win today:

- `openapi3filter.AuthenticationFunc` returns only an `error` and cannot write to the downstream request context, so a thin `RequireAuth` must survive regardless to inject the user ID. The change removes the route list, not the middleware.
- Validator errors need an explicit `ErrorHandler` mapping into `problem+json`, or error bodies become inconsistent with [ADR 0019](/docs/adr/0019-stdlib-http-router.md).
- `format:` is not validated unless explicitly enabled and `email` is not built in, so the service layer keeps its semantic validation either way.
- `kin-openapi` is pre-v1 and breaks between minor versions ([ADR 0017](/docs/adr/0017-hybrid-dependency-management.md)).

### Spec validation on upgrade

"Enforcing the reproducibility claim (CI)" above deferred native spec validation to the v2.8.0 upgrade. That upgrade has now happened and the behaviour is on by default - no configuration - so the commitment is met: v2.8.0 refuses to generate from a spec with an unresolvable `$ref` rather than emitting quietly broken output, and the `codegen` job inherits that check for free. This remains hygiene, not a security control; the spec-trust reasoning above is unchanged.

### Path prefix

Related mechanism, decided at the same time: the `/api/v1` prefix lives in the spec's `paths` keys, **not** in `StdHTTPServerOptions.BaseURL`. `BaseURL` prefixes every registered route uniformly, which would move `/healthz` and `/readyz` (and `/metrics`, once wired) under `/api/v1` - they are probe and scrape targets, not part of the versioned contract, and must not move when the contract version does. Keeping the prefix in the spec also means the committed document shows the real paths rather than deferring them to Go wiring. The versioning rationale itself is an API-design decision rather than a codegen one; if it needs recording, it belongs in its own ADR or in [`architecture.md`](/docs/architecture.md).

## Alternatives considered

- **Hand-written types and interfaces.** Rejected: duplicates the spec, drifts from it over time, and contradicts [ADR 0004](/docs/adr/0004-rest-over-grpc.md)'s spec-first commitment.
- **A framework generator (`chi-server`, etc.).** Rejected: pulls in the router dependency [ADR 0019](/docs/adr/0019-stdlib-http-router.md) deliberately declined.
- **The plain (non-strict) `std-http-server` interface.** Rejected. It hands handlers the raw `http.ResponseWriter`/`*http.Request`, leaving body decoding and status-code selection by hand - more boilerplate and no compile-time guard on the response shape. Its only advantage is a smaller generated surface, which does not matter here. Adopting it first and switching to `strict-server` later was the original plan; we rejected that sequencing because the migration cost rises with every handler added while the switch is free today, so "defer and revisit" is the more expensive path (see Decision).
- **One generated package per bounded context.** Rejected: `oapi-codegen` emits one interface per document; per-context packages would require splitting the spec or post-processing the output - more machinery than a composition-root aggregate.
