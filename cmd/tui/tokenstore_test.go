package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/openapi"
)

// A fixed instant, so every expiry assertion is exact arithmetic rather than a
// window around time.Now(). Save takes now as a parameter precisely so this is
// possible.
var testIssued = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

// The response shape both Register and Login return.
var testResponse = openapi.TokenResponse{
	Token:     "jwt-abc",
	TokenType: "Bearer",
	ExpiresIn: 3600,
}

// The on-disk filename is part of the contract - a user has to be able to find
// and delete this file by hand - so the tests name it rather than calling
// s.path(). Deriving it the same way the code does would assert nothing.
const testFilename = "token.json"

func TestTokenStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if err := NewTokenStoreIn(dir).Save(testResponse, testIssued); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	// A second store over the same directory, not the one that saved: this is
	// the restart the slice's done-condition describes, and it is what would
	// catch a store that answered from memory instead of from the file.
	got, err := NewTokenStoreIn(dir).Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if got.Token != testResponse.Token {
		t.Errorf("Token = %q, want %q", got.Token, testResponse.Token)
	}

	// expires_in is relative seconds; expires_at is the absolute instant it
	// resolved to. 3600 seconds, not 3600 nanoseconds - the conversion in
	// Save is the one place that distinction can be silently lost.
	want := testIssued.Add(time.Hour)
	if !got.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want)
	}
}

func TestTokenStoreSaveWritesExpectedFile(t *testing.T) {
	// A directory that does not exist yet, two levels down: the fresh-machine
	// path, where nothing has ever written state before.
	dir := filepath.Join(t.TempDir(), "state", "typist")

	if err := NewTokenStoreIn(dir).Save(testResponse, testIssued); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	info, err := os.Stat(filepath.Join(dir, testFilename))
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("token path is a directory, want a file")
	}

	// The file is a bearer credential. Asserting the absence of group and
	// other bits rather than == 0o600 keeps this true under any umask.
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("token file mode = %#o, want no group or other bits", perm)
	}

	// The directory holds a credential too, and MkdirAll applies the umask,
	// so this asserts the same way.
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat token directory: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("token directory mode = %#o, want no group or other bits", perm)
	}
}

func TestTokenStoreLoadMissing(t *testing.T) {
	_, err := NewTokenStoreIn(t.TempDir()).Load()
	if err == nil {
		t.Fatalf("Load() error = nil, want an error")
	}

	// Not merely "an error": the startup path routes to the login screen on
	// exactly this sentinel, which survives only because Load wraps with %w.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load() error = %v, want it to match fs.ErrNotExist", err)
	}
}

func TestTokenStoreLoadMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, testFilename), []byte(`{"token":`), 0o600); err != nil {
		t.Fatalf("writing malformed token file: %v", err)
	}

	got, err := NewTokenStoreIn(dir).Load()
	if err == nil {
		// A zero storedToken is an empty token string, which the client would
		// send as "Bearer " and the server would answer 401 - a truncated
		// file has to fail here, not three screens later.
		t.Fatalf("Load() = %+v, error = nil, want an error", got)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load() error = %v, want a decode failure, not fs.ErrNotExist", err)
	}
}

func TestStoredTokenLive(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "an hour from now is live",
			expiresAt: testIssued.Add(time.Hour),
			want:      true,
		},
		{
			// The case the startup path exists for, and the reason expiry is
			// stored absolute: a token written last week is dead without a
			// round trip to find out.
			name:      "an hour ago is not live",
			expiresAt: testIssued.Add(-time.Hour),
			want:      false,
		},
		{
			// Exactly at the boundary. Before is strict, so the token is
			// already dead - the safe direction, since the server's own exp
			// claim is the authority and a 401 is the cost of guessing wrong.
			name:      "exactly now is not live",
			expiresAt: testIssued,
			want:      false,
		},
		{
			// The zero value, which is what a Load of a JSON object with no
			// expires_at member yields. Year 1 is in the past, so it reads as
			// dead rather than as live forever.
			name:      "the zero value is not live",
			expiresAt: time.Time{},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storedToken{Token: "jwt-abc", ExpiresAt: tt.expiresAt}.Live(testIssued)
			if got != tt.want {
				t.Errorf("Live() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStateDir(t *testing.T) {
	// t.Setenv restores the previous value at the end of the test and makes
	// the test fail if it is ever run in parallel, which is what keeps these
	// cases from leaking into each other.
	tests := []struct {
		name string
		// The environment the case runs under. An empty string means unset.
		xdgStateHome string
		home         string
		// Path elements the result must be, joined under the temp home when
		// underHome is set, or absolute as written.
		want      []string
		underHome bool
	}{
		{
			name:         "an absolute XDG_STATE_HOME wins",
			xdgStateHome: "/var/tmp/xdg-state",
			// The application subdirectory is appended here too. Returning
			// XDG_STATE_HOME bare would put token.json in a directory shared
			// with every other application on the machine.
			want: []string{"/var/tmp/xdg-state", "typist"},
		},
		{
			// The XDG spec says a relative value must be treated as unset.
			// Honouring it would resolve the token file against whatever
			// directory the client happened to be launched from.
			name:         "a relative XDG_STATE_HOME is ignored",
			xdgStateHome: "relative/state",
			want:         []string{".local", "state", "typist"},
			underHome:    true,
		},
		{
			name:         "unset falls back to the home directory",
			xdgStateHome: "",
			want:         []string{".local", "state", "typist"},
			underHome:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", tt.xdgStateHome)

			got, err := stateDir()
			if err != nil {
				t.Fatalf("stateDir() error = %v, want nil", err)
			}

			want := filepath.Join(tt.want...)
			if tt.underHome {
				want = filepath.Join(append([]string{home}, tt.want...)...)
			}
			if got != want {
				t.Errorf("stateDir() = %q, want %q", got, want)
			}
		})
	}
}
