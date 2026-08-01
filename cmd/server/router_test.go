package main

import (
	"context"
	"errors"
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

	tests := []struct {
		name       string
		method     string
		target     string
		ready      func(context.Context) error
		wantStatus int
		wantCType  string // "" = don't check
	}{
		{
			"healthz",
			"GET",
			"/healthz",
			readyOK,
			200,
			"application/json",
		},
		{
			"readyz up",
			"GET",
			"/readyz",
			readyOK,
			200,
			"application/json",
		},
		{
			"readyz down",
			"GET", "/readyz",
			readyDown,
			503,
			"application/problem+json",
		},
		{
			// The 405 is emitted by http.ServeMux itself before any handler
			// runs, so it carries the stdlib's default text/plain body rather
			// than problem+json. See ADR 0019 for why this is accepted.
			"wrong method",
			"POST", "/healthz",
			readyOK,
			405,
			"text/plain; charset=utf-8",
		},
		{
			// The auth gate rejects before the handler runs, so this needs no
			// progress service wired in.
			"protected route without a token",
			"GET", "/api/v1/progress",
			readyOK,
			401,
			"application/problem+json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
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
