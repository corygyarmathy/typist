package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/corygyarmathy/typist/internal/openapi"
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

	tests := map[string]struct {
		setMarker  bool   // true = protected op (marker planted); false = public
		authHeader string // "" means no header sent
		fake       *fakeValidator
		wantStatus int  // http.StatusOK (next ran) or http.StatusUnauthorized
		wantNext   bool // did next.ServeHTTP run?
	}{
		"public op passes through": {
			setMarker:  false,            // no marker -> gate lets it through
			fake:       &fakeValidator{}, // never consulted
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		"protected, missing token": {
			setMarker:  true,
			authHeader: "",
			fake:       &fakeValidator{},
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		"protected, malformed scheme": {
			setMarker:  true,
			authHeader: "garbage-no-space",
			fake:       &fakeValidator{},
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		"protected, validator rejects": {
			setMarker:  true,
			authHeader: "Bearer good.token.here",
			fake:       &fakeValidator{err: genericError},
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		"protected, valid token": {
			setMarker:  true,
			authHeader: "Bearer good.token.here",
			fake:       &fakeValidator{userID: wantID},
			wantStatus: http.StatusOK,
			wantNext:   true,
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

			// b) the request
			req := httptest.NewRequest(http.MethodGet, "/progress", nil)
			// set the Authorization header if the case has one:
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			// and, for a PROTECTED op, plant the marker the generated wrapper would plant.
			// The key is oapi-codegen's generated string const; our middleware reads that
			// exact key, so we must plant that exact key here. A custom typed key (SA1029's
			// usual fix) would no longer match what the production wrapper stores.
			if tc.setMarker {
				ctx := context.WithValue(req.Context(), openapi.BearerAuthScopes, []string{}) //nolint:staticcheck // SA1029: key type fixed by generated code
				req = req.WithContext(ctx)
			}

			// c) a recorder to capture the response
			rec := httptest.NewRecorder()

			// d) build the middleware around the spy and serve
			RequireAuth(tc.fake)(spy).ServeHTTP(rec, req)

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
