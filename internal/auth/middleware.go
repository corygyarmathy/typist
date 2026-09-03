package auth

import (
	"net/http"
	"strings"

	"github.com/corygyarmathy/typist/internal/platform/httpx"
	"github.com/google/uuid"
)

// Validator is the slice of the authenticator the middleware needs. Defining
// it here (the consumer) - not next to Authenticator - lets tests supply a fake.
type Validator interface {
	Validate(token string) (uuid.UUID, error)
}

// RequireAuth validates the bearer token on every request whose matched route
// is not listed in public, and injects the resulting user ID into the request
// context.
//
// public is keyed by http.ServeMux pattern ("GET /healthz") - the value
// r.Pattern holds once the mux has routed the request. Route composition is
// cmd/server's job, so the set is passed in rather than owned here.
//
// The set lists PUBLIC routes, so the default is deny: a route added to the
// spec but not to the set fails closed with a 401 rather than silently serving
// without a token. TestRouter_SpecDrift keeps the set honest against
// api/openapi.yaml.
//
// Before oapi-codegen v2.8.0 this gate read a context marker the generated
// wrapper planted on operations carrying a `security` block. Upstream removed
// it (oapi-codegen#2440) because a flattened scope list cannot express
// alternative (OR) or combined (AND) security requirements. r.Pattern answers
// the same question - "which route is this?" - straight from the stdlib.
func RequireAuth(v Validator, public map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			// Public routes skip validation but must still reach the handler.
			// r.Pattern is "" when no mux routed the request; that is not in the
			// set, so an unrouted request is treated as protected.
			if public[r.Pattern] {
				next.ServeHTTP(w, r)
				return
			}

			id, detail, ok := Authenticate(v, r)
			if !ok {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, detail)
				return
			}

			// Success: inject the id so handlers can read it back out with
			// httpx.UserIDFromContext.
			ctx := httpx.WithUserID(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Authenticate resolves the caller's user ID from the request's bearer token.
// On failure it returns ok=false plus the detail to report in the 401; it
// never writes to a ResponseWriter, so a caller that is not the middleware can
// use it to ask "would this request have been let through?".
//
// It is exported for exactly one such caller: cmd/server's ErrorHandlerFunc,
// which oapi-codegen invokes for query-parameter binding failures before the
// middleware chain runs (see the note in cmd/server/router.go). Pulling the
// decision out here rather than re-parsing the header there is what stops a
// security check from existing in two places and drifting apart.
//
// It deliberately does not consult the public-route set: whether a route needs
// a token is route composition's business, and both callers already know the
// answer before they get here.
func Authenticate(v Validator, r *http.Request) (uuid.UUID, string, bool) {
	const malformed = "missing or malformed bearer token"

	// Pull the "Authorization" header. Missing -> 401.
	authHead := r.Header.Get("Authorization")
	if authHead == "" {
		return uuid.Nil, malformed, false
	}

	// Split into scheme + token on the first space. The scheme must match
	// "Bearer" case-insensitively (RFC 7235) -> strings.EqualFold.
	scheme, token, ok := strings.Cut(authHead, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return uuid.Nil, malformed, false
	}

	id, err := v.Validate(token)
	if err != nil {
		return uuid.Nil, "authentication token failed to be validated", false
	}

	return id, "", true
}
