// The middleware here is composed by cmd/server (see Router). RequestID must be
// outermost so every request — including ones that error or panic further in —
// carries an ID for log correlation; Logging sits inside it to read that ID
// back, and Recovery innermost to catch panics from the real handlers. Auth
// (JWT validation + user-context injection) is wired separately in cmd/server
// as an oapi middleware, so it can read the generated per-route security marker.

package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	requestIDHeader = "X-Request-Id"
	// maxRequestIDLen bounds an inbound ID so an attacker-controlled header
	// can't bloat every log line for the request.
	maxRequestIDLen = 128
)

// MaxBytes returns middleware that caps each request body at n bytes. Reading
// past the limit makes the body return an error, which downstream JSON decoding
// surfaces as a 400 - so a client can't stream an unbounded body into memory.
// The current API bodies are small JSON documents; n is a generous ceiling, not
// a tight per-route limit (routes can tighten it later if needed).
func MaxBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID is middleware that generates X-Request-Id if absent, stores into
// request context. Outermost middleware to ensure all requests have an ID so
// they can be tracked in the code.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) { // honour a sane inbound ID; else mint one
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)   // echo to the client
		ctx := WithRequestID(r.Context(), id) // AND stash for our own code
		next.ServeHTTP(w, r.WithContext(ctx)) // pass the NEW request down
	})
}

// validRequestID reports whether an inbound ID is safe to echo and log: a
// non-empty, bounded string of printable ASCII. Anything else is replaced
// with a freshly generated ID.
func validRequestID(s string) bool {
	// valid length
	if s == "" || len(s) > maxRequestIDLen {
		return false
	}
	// valid characters
	for _, c := range s {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// newRequestID returns a random UUID string, assumed to be unique
func newRequestID() string {
	return uuid.NewString()
}

// Logging middleware constructs a wrapper to record the status of actioned
// requests, such that they can be logged. Next inner from RequestID so that
// all requests are logged.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r) // pass the status recorder, not w

		// log request
		slog.Info("http request",
			"request_id", RequestIDFromContext(r.Context()), // reads what RequestID stashed
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter // inherit Header(), Write(), WriteHeader()
	status              int
	bytes               int
	wrote               bool
}

// Overwrite WriteHeader() so status is recorded
func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.wrote = true
	rec.ResponseWriter.WriteHeader(code)
}

// Overwrite Write() so write state recorded
func (rec *statusRecorder) Write(b []byte) (int, error) {
	rec.wrote = true
	n, err := rec.ResponseWriter.Write(b)
	rec.bytes += n
	return n, err
}

// Recovery middleware handles panics, logging and returning error info.
// If no panic, continue. If panic:
// - Check if handler intentionally aborted, re-panic
// - Log error & write problem
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return // normal path: nothing panicked
			}
			if rec == http.ErrAbortHandler {
				panic(rec) // intentional abort: re-panic, don't swallow
			}
			slog.Error("panic recovered",
				"request_id", RequestIDFromContext(r.Context()),
				"panic", rec,
				"path", r.URL.Path,
			)
			if !responseStarted(w) {
				WriteProblem(w, r, http.StatusInternalServerError, "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// responseStarted checks if ResponseWrite has wrote yet
func responseStarted(w http.ResponseWriter) bool {
	rec, ok := w.(*statusRecorder)
	return ok && rec.wrote
}
