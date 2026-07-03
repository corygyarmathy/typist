package main

import (
	"context"
	"net/http"
	"time"

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
func Router(ready func(ctx context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(ready))
	// Middleware order (outer -> inner): RequestID, Logging, Recovery.
	return chain(mux, httpx.RequestID, httpx.Logging, httpx.Recovery)
}

// healthz returns status ok; basic check that the API JSON response writer is
// functioning.
func healthz(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// readyz confirms the database is reachable, returning status ok if the db
// ping succeeds within 2 seconds.
func readyz(ready func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// attempt to ping db; return service unavailable if it times out
		if err := ready(ctx); err != nil {
			httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "database is not reachable")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}
