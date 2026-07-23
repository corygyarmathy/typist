package auth

import (
	"net/http"
	"strings"

	"github.com/corygyarmathy/typist/internal/openapi"
	"github.com/corygyarmathy/typist/internal/platform/httpx"
	"github.com/google/uuid"
)

// Validator is the slice of the authenticator the middleware needs. Defining
// it here (the consumer) - not next to Authenticator - lets tests supply a fake.
type Validator interface {
	Validate(token string) (uuid.UUID, error)
}

func RequireAuth(v Validator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. GATE: is this a protected op? If openapi.BearerAuthScopes is
			//    NOT in r.Context(), it's public → next.ServeHTTP and return.
			//    (type-assert ctx.Value(openapi.BearerAuthScopes) to []string)
			if _, ok := r.Context().Value(openapi.BearerAuthScopes).([]string); !ok {
				next.ServeHTTP(w, r)
				return
			}

			// 2. Pull the "Authorization" header. Missing → 401.
			authHead := r.Header.Get("Authorization")
			if authHead == "" {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "missing or malformed bearer token")
				return
			}

			// 3. Split into scheme + token on the first space. The scheme must
			//    match "Bearer" CASE-INSENSITIVELY (RFC 7235) → strings.EqualFold.
			//    Malformed → 401.
			scheme, token, ok := strings.Cut(authHead, " ")
			if !ok {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "missing or malformed bearer token")
				return
			}
			if !strings.EqualFold(scheme, "Bearer") {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "missing or malformed bearer token")
				return
			}

			// 4. v.Validate(token). Error → 401.
			id, err := v.Validate(token)
			if err != nil {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "authentication token failed to be validated")
				return
			}

			// 5. Success: inject the id with httpx.WithUserID, then
			//    next.ServeHTTP(w, r.WithContext(ctx)).
			ctx := httpx.WithUserID(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
