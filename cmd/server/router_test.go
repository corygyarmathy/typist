package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
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
