package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		},
		{
			name:       "200 with a body that is not JSON",
			status:     http.StatusOK,
			ctype:      "application/json",
			body:       `{"words": [`,
			wantErr:    true,
			wantErrHas: []string{"decoding"},
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
