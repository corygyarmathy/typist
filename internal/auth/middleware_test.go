package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/corygyarmathy/typist/internal/platform/httpx"
	"github.com/google/uuid"
)

type fakeValidator struct {
	userID uuid.UUID // 'returned' user ID
	err    error     // specify error to be 'returned'
}

func (v *fakeValidator) Validate(token string) (uuid.UUID, error) {
	return v.userID, v.err
}

func TestRequireAuth(t *testing.T) {
	wantID := uuid.New()
	genericError := errors.New("this is a generic error")

	// The set the middleware gates on. Only the one entry matters here; the
	// real set lives in cmd/server and is checked against the spec by
	// TestRouter_SpecDrift.
	public := map[string]bool{"GET /healthz": true}

	const (
		publicPattern    = "GET /healthz"
		protectedPattern = "GET /api/v1/progress"
	)

	tests := map[string]struct {
		pattern    string // http.ServeMux pattern the mux would have matched
		authHeader string // "" means no header sent
		fake       *fakeValidator
		wantStatus int  // http.StatusOK (next ran) or http.StatusUnauthorized
		wantNext   bool // did next.ServeHTTP run?
	}{
		"public op passes through": {
			pattern:    publicPattern,
			fake:       &fakeValidator{}, // never consulted
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		"protected, missing token": {
			pattern:    protectedPattern,
			authHeader: "",
			fake:       &fakeValidator{},
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		"protected, malformed scheme": {
			pattern:    protectedPattern,
			authHeader: "garbage-no-space",
			fake:       &fakeValidator{},
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		"protected, validator rejects": {
			pattern:    protectedPattern,
			authHeader: "Bearer good.token.here",
			fake:       &fakeValidator{err: genericError},
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		"protected, valid token": {
			pattern:    protectedPattern,
			authHeader: "Bearer good.token.here",
			fake:       &fakeValidator{userID: wantID},
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		// An unrouted request carries no pattern. Default-deny means that is
		// treated as protected rather than waved through.
		"empty pattern is treated as protected": {
			pattern:    "",
			authHeader: "",
			fake:       &fakeValidator{},
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {

			// a) the spy: your fake "protected handler"
			var nextCalled bool
			var gotUserID uuid.UUID
			spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				gotUserID, _ = httpx.UserIDFromContext(r.Context()) // did the mw inject it?
				w.WriteHeader(http.StatusOK)                        // the "success" status
			})

			// b) the request. Pattern is normally set by http.ServeMux once it
			// has matched a route; setting it directly is what lets this test
			// exercise the gate without standing up a mux.
			req := httptest.NewRequest(http.MethodGet, "/api/v1/progress", nil)
			req.Pattern = tc.pattern
			// set the Authorization header if the case has one:
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			// c) a recorder to capture the response
			rec := httptest.NewRecorder()

			// d) build the middleware around the spy and serve
			RequireAuth(tc.fake, public)(spy).ServeHTTP(rec, req)

			// assertions: rec.Code, nextCalled, gotUserID
			if rec.Code != tc.wantStatus {
				t.Errorf("wanted status %v, got %v", tc.wantStatus, rec.Code)
			}
			if nextCalled != tc.wantNext {
				t.Errorf("next HTTP handler unexpectedly not called")
			}
			if gotUserID != tc.fake.userID {
				t.Errorf("did not receive wanted user ID, wanted '%v', got '%v'", wantID, gotUserID)
			}
		})
	}
}
