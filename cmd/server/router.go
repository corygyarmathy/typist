package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/openapi"
	"github.com/corygyarmathy/typist/internal/platform/httpx"
)

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
				case errors.Is(err, auth.ErrReqBodyEmpty):
					httpx.WriteProblem(w, r, http.StatusBadRequest, "the request body is empty")
				case errors.Is(err, auth.ErrInvalidCredentials):
					httpx.WriteProblem(w, r, http.StatusUnauthorized, "invalid email or password provided")
				default:
					// TODO: the central handler should slog.Error the raw err on the default branch before writing the opaque response.
					httpx.WriteProblem(w, r, http.StatusInternalServerError, "unexpected error when calling handler")
				}
			},
		},
	)
	handler := openapi.HandlerWithOptions(
		si,
		openapi.StdHTTPServerOptions{
			BaseRouter:  mux,
			Middlewares: []openapi.MiddlewareFunc{auth.RequireAuth(v)},
		})

	// Middleware order (outer -> inner): RequestID, Logging, Recovery.
	return chain(handler, httpx.RequestID, httpx.Logging, httpx.Recovery)
}
