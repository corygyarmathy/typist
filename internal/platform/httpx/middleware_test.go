package httpx

//TODO: validate, understand, and implement these tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_HonoursSaneInbound(t *testing.T) {
	var got string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = w.Header().Get(requestIDHeader)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "client-supplied-123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got != "client-supplied-123" {
		t.Errorf("request id = %q, want the inbound value echoed", got)
	}
}

func TestRequestID_ReplacesInvalidInbound(t *testing.T) {
	tests := map[string]string{
		"empty":         "",
		"too long":      strings.Repeat("a", maxRequestIDLen+1),
		"control chars": "bad\r\ninjected",
	}
	for name, inbound := range tests {
		t.Run(name, func(t *testing.T) {
			var got string
			h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = w.Header().Get(requestIDHeader)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if inbound != "" {
				req.Header.Set(requestIDHeader, inbound)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if got == inbound {
				t.Errorf("invalid inbound %q was honoured", inbound)
			}
			if !validRequestID(got) {
				t.Errorf("generated id %q is not valid", got)
			}
		})
	}
}

func TestRecovery_WritesProblemOnPanic(t *testing.T) {
	h := Recovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}
}

func TestRecovery_RepanicsAbortHandler(t *testing.T) {
	h := Recovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("ErrAbortHandler was not re-panicked; recovered %v", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("expected ErrAbortHandler to propagate")
}

func TestRecovery_DoesNotOverwriteStartedResponse(t *testing.T) {
	// Logging installs the wrapper that lets Recovery see a started reply.
	h := Logging(Recovery(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("partial"))
		panic("after write")
	})))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418 (Recovery must not overwrite a started response)", rr.Code)
	}
	if body := rr.Body.String(); body != "partial" {
		t.Errorf("body = %q, want %q (no problem body appended)", body, "partial")
	}
}
