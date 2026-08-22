package main

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type noopValidator struct{}

func (noopValidator) Validate(string) (uuid.UUID, error) { return uuid.Nil, nil }

func TestRouter(t *testing.T) {
	readyOK := func(context.Context) error { return nil }
	readyDown := func(context.Context) error { return errors.New("db down") }

	// Keyed fields, so a case states only what it cares about and the zero
	// value means "no token" / "no body".
	tests := []struct {
		name       string
		method     string
		target     string
		token      bool   // send "Authorization: Bearer test-token"
		body       string // request body; "" sends none
		ready      func(context.Context) error
		wantStatus int
		wantCType  string // "" = don't check
	}{
		{
			name:       "healthz",
			method:     "GET",
			target:     "/healthz",
			ready:      readyOK,
			wantStatus: 200,
			wantCType:  "application/json",
		},
		{
			name:       "readyz up",
			method:     "GET",
			target:     "/readyz",
			ready:      readyOK,
			wantStatus: 200,
			wantCType:  "application/json",
		},
		{
			name:       "readyz down",
			method:     "GET",
			target:     "/readyz",
			ready:      readyDown,
			wantStatus: 503,
			wantCType:  "application/problem+json",
		},
		{
			// The 405 is emitted by http.ServeMux itself before any handler
			// runs, so it carries the stdlib's default text/plain body rather
			// than problem+json. See ADR 0019 for why this is accepted.
			name:       "wrong method",
			method:     "POST",
			target:     "/healthz",
			ready:      readyOK,
			wantStatus: 405,
			wantCType:  "text/plain; charset=utf-8",
		},
		{
			// The auth gate rejects before the handler runs, so this needs no
			// progress service wired in.
			name:       "protected route without a token",
			method:     "GET",
			target:     "/api/v1/progress",
			ready:      readyOK,
			wantStatus: 401,
			wantCType:  "application/problem+json",
		},

		// The phase-4 surface. Without a token: 401, with a token: 501.
		{
			name:       "next lesson without a token",
			method:     "GET",
			target:     "/api/v1/lessons/next",
			ready:      readyOK,
			wantStatus: 401,
			wantCType:  "application/problem+json",
		},
		{
			name:       "list sessions without a token",
			method:     "GET",
			target:     "/api/v1/sessions?limit=5",
			ready:      readyOK,
			wantStatus: 401,
			wantCType:  "application/problem+json",
		},
		{
			name:       "list sessions stub",
			method:     "GET",
			target:     "/api/v1/sessions?limit=5",
			token:      true,
			ready:      readyOK,
			wantStatus: 501,
			wantCType:  "application/problem+json",
		},
		{
			name:       "submit session without a token",
			method:     "POST",
			target:     "/api/v1/sessions",
			body:       `{"keys":{},"ngrams":{}}`,
			ready:      readyOK,
			wantStatus: 401,
			wantCType:  "application/problem+json",
		},
		{
			// The body must at least parse as SessionSubmission: the generated
			// strict handler decodes it before calling *API, so a malformed
			// body never reaches the stub. It short-circuits through
			// RequestErrorHandlerFunc instead - see the case below.
			name:       "submit session stub",
			method:     "POST",
			target:     "/api/v1/sessions",
			token:      true,
			body:       `{"keys":{},"ngrams":{}}`,
			ready:      readyOK,
			wantStatus: 501,
			wantCType:  "application/problem+json",
		},

		// The two error paths the generated code owns rather than *API. Both
		// default upstream to http.Error (text/plain, raw error text);
		// router.go overrides them, and these cases pin the override.
		{
			// Body decode fails inside the strict handler, which runs AFTER
			// RequireAuth - so this needs a token to reach the failure at all.
			name:       "submit session with a malformed body",
			method:     "POST",
			target:     "/api/v1/sessions",
			token:      true,
			body:       `{"keys":`,
			ready:      readyOK,
			wantStatus: 400,
			wantCType:  "application/problem+json",
		},
		{
			name:       "list sessions with an unparseable limit",
			method:     "GET",
			target:     "/api/v1/sessions?limit=abc",
			token:      true,
			ready:      readyOK,
			wantStatus: 400,
			wantCType:  "application/problem+json",
		},
		{
			// Parameter binding runs BEFORE the middleware chain, so without
			// the re-check in ErrorHandlerFunc this answers 400 and names the
			// limit parameter to an anonymous caller. 401 must win.
			name:       "unparseable limit without a token is still a 401",
			method:     "GET",
			target:     "/api/v1/sessions?limit=abc",
			ready:      readyOK,
			wantStatus: 401,
			wantCType:  "application/problem+json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.target, body)
			if tt.token {
				// noopValidator accepts anything; the gate only cares that a
				// well-formed Bearer header is present and validates.
				req.Header.Set("Authorization", "Bearer test-token")
			}
			rec := httptest.NewRecorder()

			Router(&API{ready: tt.ready}, noopValidator{}).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantCType != "" && rec.Header().Get("Content-Type") != tt.wantCType {
				t.Errorf("content-type = %q, want %q", rec.Header().Get("Content-Type"), tt.wantCType)
			}
		})
	}
}

// specPath is one path item from the spec. Operations are decoded lazily via
// yaml.Node so non-operation keys (parameters, summary, ...) do not break the
// decode when they are eventually added.
type specPath map[string]yaml.Node

type specDoc struct {
	Paths map[string]specPath `yaml:"paths"`
}

type specOperation struct {
	// Pointer so an ABSENT `security` (nil - inherits the top-level bearerAuth
	// default, therefore protected) stays distinguishable from an explicit
	// `security: []` (non-nil but empty - therefore public). A plain slice
	// collapses both to len 0 and every route reads as public.
	Security *[]map[string][]string `yaml:"security"`
}

// httpMethods are the OpenAPI path-item keys that denote an operation. Anything
// else under a path item is metadata and is skipped.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// TestRouter_SpecDrift pins publicRoutes to api/openapi.yaml.
//
// oapi-codegen v2.8.0 stopped emitting the per-operation security marker the
// auth middleware used to gate on (oapi-codegen#2440), so the public/protected
// split is now expressed in Go rather than derived from the spec at runtime.
// This test is what stops the two from diverging: the spec stays the source of
// truth, and a mismatch fails the build instead of silently changing which
// endpoints need a token.
func TestRouter_SpecDrift(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc specDoc
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("spec parsed but contains no paths; the test is not looking at what it thinks it is")
	}

	// Collect the operations that opt out of the top-level security default.
	want := map[string]bool{}
	for path, item := range doc.Paths {
		for key, node := range item {
			if !httpMethods[strings.ToLower(key)] {
				continue
			}
			var op specOperation
			if err := node.Decode(&op); err != nil {
				t.Fatalf("decode %s %s: %v", key, path, err)
			}
			if op.Security != nil && len(*op.Security) == 0 {
				want[strings.ToUpper(key)+" "+path] = true
			}
		}
	}

	if !maps.Equal(want, publicRoutes) {
		t.Errorf("publicRoutes disagrees with api/openapi.yaml\n"+
			"spec says public (security: []): %v\n"+
			"publicRoutes says public:         %v",
			keys(want), keys(publicRoutes))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
