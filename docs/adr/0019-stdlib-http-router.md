# ADR 0019: Use the standard library HTTP router

- **Status:** Accepted
- **Date:** 2026-07-02
- **Related Artefacts:**
  - Implements: `cmd/server/router.go`
  - Constrains: `api/openapi.yaml` (error contract)

## Context

The API needs request routing, including method-aware matching (`GET /healthz` vs `POST /healthz`). Go 1.22 added method and wildcard patterns to the standard library `http.ServeMux`, closing most of the gap that historically pushed projects toward third-party routers such as `chi` or `gorilla/mux`.

Every third-party router is a dependency to vet, upgrade, and justify. At this stage the routing needs are modest, and I could not yet defend the cost of an extra dependency over the standard library - nor did I want to adopt an abstraction before understanding the layer it hides.

## Decision

Use `net/http`'s `http.ServeMux` with Go 1.22 method patterns for routing. Compose cross-cutting behaviour (request ID, logging, recovery) as plain `func(http.Handler) http.Handler` middleware rather than router-specific hooks.

## Consequences

**Positive**

- No routing dependency to track, audit, or upgrade.
- Middleware is standard-library-shaped, so it stays portable if the router is later swapped.
- Forces first-hand understanding of the routing layer before any abstraction is layered on top.

**Negative**

- `http.ServeMux` emits its own `404 Not Found` and `405 Method Not Allowed` responses _before dispatch_, using the stdlib default `text/plain` body. There is no hook to customise them. This means those two status codes escape the `application/problem+json` error contract - the `WriteProblem` writer never runs for them. Accept the inconsistency for now; it is documented in `api/openapi.yaml` and asserted in `internal/api/router_test.go`.
- Method matching sugar is fixed; behaviours a richer router offers (route groups, typed params) must be hand-rolled if needed.

## Alternatives considered

- **chi.** Would give customisable `NotFound`/`MethodNotAllowed` handlers, letting 404/405 return `problem+json` and closing the error-contract gap above. Rejected for now: the only concrete benefit is uniform error bodies on two status codes, which does not yet justify the dependency.

## Future option (revisit trigger: the error contract must be uniform)

If a client (or codegen against `openapi.yaml`) requires _every_ error to be `problem+json`, close the gap without adding a dependency by buffering the response in middleware: wrap the `ResponseWriter`, and when the inner handler produced a `text/plain` 404/405, rewrite the buffered body to `problem+json` before flushing. Buffering is required because status and headers are committed on `WriteHeader` and cannot be changed after the fact. If that middleware grows awkward, re-evaluate `chi` at that point.
