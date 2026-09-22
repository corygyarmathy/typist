package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corygyarmathy/typist/internal/openapi"
)

func TestClientNextLesson(t *testing.T) {
	const testToken = "test-token"

	// Keyed fields: a case states only what it cares about,
	// and the zero value means "don't check".
	tests := []struct {
		name string

		// What the fake server sends back.
		status int
		ctype  string
		body   string

		// What NextLesson is expected to produce.
		wantWords []string
		wantErr   bool
		// Substrings the error must contain.
		wantErrHas []string
		// Substrings the error must NOT contain. A Contains check alone
		// passes whether errorFromResponse decoded the problem or dumped the
		// raw body verbatim, because the raw body holds the same substrings.
		wantErrLacks []string
		// Status the recovered *statusError must carry. 0 means don't check.
		wantStatus int
	}{
		{
			name:   "200 decodes the lesson",
			status: http.StatusOK,
			ctype:  "application/json",
			// A raw literal, not json.Marshal(openapi.Lesson{...}). Encoding
			// the struct and decoding it back would pass even if the json
			// tags were wrong, because the same wrong tags cancel out. This
			// asserts the wire format from api/openapi.yaml.
			body:      `{"words":["the","and","for"],"targets":["e","t"]}`,
			wantWords: []string{"the", "and", "for"},
		},
		{
			name:   "401 surfaces the problem detail",
			status: http.StatusUnauthorized,
			// The shape httpx.WriteProblem emits. Note the "; charset"
			// - a Content-Type check that uses == rather than a
			// prefix/mime parse would miss this.
			ctype: "application/problem+json; charset=utf-8",
			body: `{"type":"about:blank","title":"Unauthorized",` +
				`"status":401,"detail":"malformed Authorization header",` +
				`"instance":"req-abc123"}`,
			wantErr: true,
			// The detail says why. The instance is the request ID, which is
			// what lets a reader grep the server log for this exact failure.
			wantErrHas: []string{"malformed Authorization header", "req-abc123"},
			// Present only in the undecoded body, so their absence is what
			// proves the problem+json branch actually ran.
			wantErrLacks: []string{"{", "about:blank"},
			wantStatus:   401,
		},
		{
			name:   "404 from the mux has no problem body",
			status: http.StatusNotFound,
			// http.ServeMux answers an unknown path itself, before any
			// handler or WriteProblem runs, so the body is the stdlib's
			// text/plain. See the note at the top of api/openapi.yaml and
			// ADR 0019. Decoding this as JSON fails - on exactly the error
			// that means "your URL is wrong".
			ctype:      "text/plain; charset=utf-8",
			body:       "404 page not found\n",
			wantErr:    true,
			wantErrHas: []string{"404"},
			wantStatus: 404,
		},
		{
			name:       "200 with a body that is not JSON",
			status:     http.StatusOK,
			ctype:      "application/json",
			body:       `{"words": [`,
			wantErr:    true,
			wantErrHas: []string{"decoding"},
		},
		{
			name:   "problem+json with no detail or instance",
			status: http.StatusUnauthorized,
			ctype:  "application/problem+json; charset=utf-8",
			// Valid, complete per the spec: detail and instance are optional
			// (openapi.Problem.Detail is *string). Before deref guarded nil,
			// this panicked the client inside its own error path.
			body:       `{"type":"about:blank","title":"Unauthorized","status":401}`,
			wantErr:    true,
			wantErrHas: []string{"Unauthorized", "401"},
			// "decoding" would mean it took the decode-failure branch instead;
			// "{" would mean it dumped the raw body.
			wantErrLacks: []string{"decoding", "{"},
			wantStatus:   401,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// These run on the server's goroutine, not the test's.
				// t.Errorf is safe from any goroutine; t.Fatal is not - it
				// calls runtime.Goexit, which would kill the handler and
				// leave the client staring at a dropped connection instead
				// of the failure message. Never t.Fatal in here.
				if got, want := r.Header.Get("Authorization"), "Bearer "+testToken; got != want {
					t.Errorf("Authorization header = %q, want %q", got, want)
				}
				if got, want := r.Method, http.MethodGet; got != want {
					t.Errorf("method = %q, want %q", got, want)
				}
				if got, want := r.URL.Path, "/api/v1/lessons/next"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}

				w.Header().Set("Content-Type", tt.ctype)
				w.WriteHeader(tt.status) // must precede the body write
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			// srv.URL is "http://127.0.0.1:<random port>" with no trailing
			// slash - the same shape NextLesson concatenates its path onto.
			lesson, err := NewClient(srv.URL, testToken).NextLesson(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Fatalf("NextLesson() error = nil, want an error")
				}
				for _, want := range tt.wantErrHas {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("NextLesson() error = %q, want it to contain %q", err, want)
					}
				}
				for _, unwanted := range tt.wantErrLacks {
					if strings.Contains(err.Error(), unwanted) {
						t.Errorf("NextLesson() error = %q, want it NOT to contain %q", err, unwanted)
					}
				}
				if tt.wantStatus != 0 {
					var se *statusError
					if !errors.As(err, &se) {
						t.Errorf("NextLesson() error = %q, want a *statusError", err)
					} else if se.Status != tt.wantStatus {
						t.Errorf("NextLesson() status = %d, want %d", se.Status, tt.wantStatus)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("NextLesson() error = %v, want nil", err)
			}
			if got, want := len(lesson.Words), len(tt.wantWords); got != want {
				t.Fatalf("len(Words) = %d, want %d", got, want)
			}
			for i, want := range tt.wantWords {
				if lesson.Words[i] != want {
					t.Errorf("Words[%d] = %q, want %q", i, lesson.Words[i], want)
				}
			}
		})
	}
}

func TestClientSubmitSession(t *testing.T) {
	const testToken = "test-token"

	// The submission every case sends. Deliberately small enough to assert
	// whole, and shaped like real accumulator output: a key with a first-try
	// error, and the bigram that error propagates into.
	sub := openapi.SessionSubmission{
		Keys: map[string]openapi.Observation{
			"a": {Attempts: 2, Errors: 1, TotalMillis: 400},
			"t": {Attempts: 1, Errors: 0, TotalMillis: 200},
		},
		Ngrams: map[string]openapi.Observation{
			"at": {Attempts: 1, Errors: 1, TotalMillis: 600},
		},
	}

	tests := []struct {
		name string

		// What the fake server sends back.
		status int
		ctype  string
		body   string

		// What SubmitSession is expected to produce.
		wantWpm      int
		wantAccuracy float64
		wantErr      bool
		wantErrHas   []string
		// Substrings the error must NOT contain. A loose Contains check
		// passes whether errorFromResponse decoded the problem or dumped the
		// raw body verbatim, because the raw body holds the same substrings.
		// Asserting the absence of a JSON artifact is what tells the two
		// apart.
		wantErrLacks []string
		// Status the recovered *statusError must carry. 0 means don't check.
		wantStatus int
	}{
		{
			name:   "201 decodes the summary",
			status: http.StatusCreated,
			ctype:  "application/json",
			// A raw literal rather than json.Marshal of a SessionSummary:
			// encoding the struct and decoding it back would pass even with
			// wrong json tags, because the same wrong tags cancel out.
			body: `{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8",` +
				`"completed_at":"2026-09-19T10:30:18Z","wpm":42,"accuracy":0.95}`,
			wantWpm:      42,
			wantAccuracy: 0.95,
		},
		{
			name: "200 is not success",
			// The spec says POST /sessions answers 201. A client that checks
			// for 2xx, or for StatusOK, would accept this and then decode an
			// empty summary into a silently wrong result screen.
			status:     http.StatusOK,
			ctype:      "application/json",
			body:       `{"wpm":42,"accuracy":0.95}`,
			wantErr:    true,
			wantErrHas: []string{"200"},
			wantStatus: 200,
		},
		{
			name:   "400 surfaces the problem detail",
			status: http.StatusBadRequest,
			ctype:  "application/problem+json; charset=utf-8",
			body: `{"type":"about:blank","title":"Bad Request","status":400,` +
				`"detail":"keys must not be empty","instance":"req-def456"}`,
			wantErr:      true,
			wantErrHas:   []string{"keys must not be empty", "req-def456"},
			wantErrLacks: []string{"{", "about:blank"},
			wantStatus:   400,
		},
		{
			name:       "201 with a body that is not JSON",
			status:     http.StatusCreated,
			ctype:      "application/json",
			body:       `{"wpm":`,
			wantErr:    true,
			wantErrHas: []string{"decoding"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// t.Errorf is safe from the server's goroutine; t.Fatal is
				// not - it calls runtime.Goexit, which would kill the handler
				// and leave the client staring at a dropped connection.
				if got, want := r.Header.Get("Authorization"), "Bearer "+testToken; got != want {
					t.Errorf("Authorization header = %q, want %q", got, want)
				}
				if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
					t.Errorf("Content-Type header = %q, want %q", got, want)
				}
				if got, want := r.Method, http.MethodPost; got != want {
					t.Errorf("method = %q, want %q", got, want)
				}
				if got, want := r.URL.Path, "/api/v1/sessions"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}

				// The request direction matters as much as the response: this
				// is what catches a wrong json tag on Observation, which no
				// assertion on the reply could see.
				var got openapi.SessionSubmission
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decoding request body: %v", err)
				} else {
					if !maps.Equal(got.Keys, sub.Keys) {
						t.Errorf("request Keys = %v, want %v", got.Keys, sub.Keys)
					}
					if !maps.Equal(got.Ngrams, sub.Ngrams) {
						t.Errorf("request Ngrams = %v, want %v", got.Ngrams, sub.Ngrams)
					}
				}

				w.Header().Set("Content-Type", tt.ctype)
				w.WriteHeader(tt.status) // must precede the body write
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			summary, err := NewClient(srv.URL, testToken).SubmitSession(context.Background(), sub)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("SubmitSession() error = nil, want an error")
				}
				for _, want := range tt.wantErrHas {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("SubmitSession() error = %q, want it to contain %q", err, want)
					}
				}
				for _, unwanted := range tt.wantErrLacks {
					if strings.Contains(err.Error(), unwanted) {
						t.Errorf("SubmitSession() error = %q, want it NOT to contain %q", err, unwanted)
					}
				}
				if tt.wantStatus != 0 {
					var se *statusError
					if !errors.As(err, &se) {
						t.Errorf("NextLesson() error = %q, want a *statusError", err)
					} else if se.Status != tt.wantStatus {
						t.Errorf("NextLesson() status = %d, want %d", se.Status, tt.wantStatus)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("SubmitSession() error = %v, want nil", err)
			}
			if got := summary.Wpm; got != tt.wantWpm {
				t.Errorf("Wpm = %d, want %d", got, tt.wantWpm)
			}
			if got := summary.Accuracy; got != tt.wantAccuracy {
				t.Errorf("Accuracy = %v, want %v", got, tt.wantAccuracy)
			}
		})
	}
}
