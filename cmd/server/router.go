package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/openapi"
	"github.com/corygyarmathy/typist/internal/platform/httpx"
)

// maxRequestBodyBytes caps every request body. The current JSON bodies are a
// few hundred bytes; this is a generous process-wide ceiling to stop an
// unbounded body from being read into memory, not a per-route limit.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// publicRoutes lists the operations that may be served without a bearer token,
// keyed by http.ServeMux pattern (the value r.Pattern carries after routing).
//
// This mirrors api/openapi.yaml, where the top-level `security` block makes
// bearerAuth the default and public operations opt out with `security: []`.
// auth.RequireAuth treats anything absent from this set as protected, so the
// failure mode for a forgotten entry is a 401, not an unguarded endpoint.
//
// TestRouter_SpecDrift asserts this set equals the spec's `security: []`
// operations, so the two cannot diverge silently.
var publicRoutes = map[string]bool{
	"GET /healthz":               true,
	"GET /readyz":                true,
	"POST /api/v1/auth/register": true,
	"POST /api/v1/auth/login":    true,
}

// chain avoids having to nest each middleware handler, increases readability.
//
// Reverses order so that the first middleware is the outermost (runs first).
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- { // apply middleware in reverse
		h = mws[i](h)
	}
	return h
}

// Router constructs the application HTTP handler.
//
// Route composition lives here in cmd/server, above the bounded contexts, so
// the shared httpx pkg stays a leaf: domain packages (auth, progress, session)
// can import httpx for response/context helpers without an import cycle forming
// when this function wires their routes in. Dependencies (services) are passed
// in here, not pulled from globals.
func Router(api *API, v auth.Validator) http.Handler {
	mux := http.NewServeMux()

	si := openapi.NewStrictHandlerWithOptions(
		api,
		nil,
		openapi.StrictHTTPServerOptions{
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				switch {
				case errors.Is(err, errNotImplemented):
					httpx.WriteProblem(w, r, http.StatusNotImplemented, "called handler is not implemented")
				case errors.Is(err, errNotReady):
					httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "database is not responsive")
				case errors.Is(err, auth.ErrEmailTaken):
					httpx.WriteProblem(w, r, http.StatusConflict, "email already registered")
				case errors.Is(err, auth.ErrInvalidEmail):
					httpx.WriteProblem(w, r, http.StatusBadRequest, "email is invalid")
				case errors.Is(err, auth.ErrPasswordTooShort):
					httpx.WriteProblem(w, r, http.StatusBadRequest,
						fmt.Sprintf("password is too short, min chars: %v", auth.MinPasswordLen),
					)
				case errors.Is(err, auth.ErrPasswordTooLong):
					httpx.WriteProblem(w, r, http.StatusBadRequest,
						fmt.Sprintf("password is too long, max chars: %v", auth.MaxPasswordLen),
					)
				case errors.Is(err, auth.ErrReqBodyEmpty):
					httpx.WriteProblem(w, r, http.StatusBadRequest, "the request body is empty")
				case errors.Is(err, auth.ErrInvalidCredentials):
					httpx.WriteProblem(w, r, http.StatusUnauthorized, "invalid email or password provided")
				default:
					// Unmapped error: log the real cause (correlated by request ID)
					// before returning an opaque 500, so the detail isn't lost.
					slog.Error("unhandled error from handler",
						"err", err,
						"request_id", httpx.RequestIDFromContext(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
					)
					httpx.WriteProblem(w, r, http.StatusInternalServerError, "unexpected error when calling handler")
				}
			},
		},
	)

	handler := openapi.HandlerWithOptions(
		si,
		openapi.StdHTTPServerOptions{
			BaseRouter:  mux,
			Middlewares: []openapi.MiddlewareFunc{auth.RequireAuth(v, publicRoutes)},
		})

	// Middleware order (outer -> inner): RequestID, Logging, Recovery, MaxBytes.
	// MaxBytes is innermost so the body cap applies just before the handler
	// reads it, while the request is still logged and panic-guarded.
	return chain(handler, httpx.RequestID, httpx.Logging, httpx.Recovery, httpx.MaxBytes(maxRequestBodyBytes))
}
