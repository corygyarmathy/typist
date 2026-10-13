package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// loginServer answers every request with status and body, and counts the
// requests so a test can tell when the login call actually happened.
func loginServer(t *testing.T, status int, ctype, body string) (*Client, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return NewClient(srv.URL, ""), &calls
}

// pressLogin runs one key through the login screen and returns the result as a
// loginScreen, so a test can read the fields back.
func pressLogin(t *testing.T, s loginScreen, key tea.KeyPressMsg) (loginScreen, tea.Cmd) {
	t.Helper()

	next, cmd := s.Update(key)
	updated, ok := next.(loginScreen)
	if !ok {
		t.Fatalf("loginScreen.Update returned %T, want loginScreen", next)
	}
	return updated, cmd
}

func typeText(t *testing.T, s loginScreen, text string) loginScreen {
	t.Helper()

	for _, r := range text {
		s, _ = pressLogin(t, s, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return s
}

var (
	tabKey   = tea.KeyPressMsg{Code: tea.KeyTab}
	enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}
)

// filledLoginScreen is a login screen with both fields typed in. The email has
// to be well formed: openapi_types.Email validates itself when marshalled, so
// an empty form fails inside the client and never reaches the server.
func filledLoginScreen(t *testing.T, client *Client) loginScreen {
	t.Helper()

	s := newLoginScreen(context.Background(), client)
	s = typeText(t, s, "a@b.c")
	s, _ = pressLogin(t, s, tabKey)
	return typeText(t, s, "hunter2")
}

// The screen opens with the email field focused, and tab moves typing to the
// password field. Both halves fail silently if a field's Update result or a
// Focus call lands on a copy that is then discarded.
func TestLoginScreenTabMovesTypingFromEmailToPassword(t *testing.T) {
	s := newLoginScreen(context.Background(), NewClient("http://example.invalid", ""))

	s = typeText(t, s, "a@b.c")
	s, _ = pressLogin(t, s, tabKey)
	s = typeText(t, s, "hunter2")

	if got, want := s.email.Value(), "a@b.c"; got != want {
		t.Errorf("email.Value() = %q, want %q", got, want)
	}
	if got, want := s.password.Value(), "hunter2"; got != want {
		t.Errorf("password.Value() = %q, want %q", got, want)
	}
}

func TestLoginScreenMasksThePassword(t *testing.T) {
	s := newLoginScreen(context.Background(), NewClient("http://example.invalid", ""))

	s, _ = pressLogin(t, s, tabKey)
	s = typeText(t, s, "hunter2")

	if content := ansi.Strip(s.View().Content); strings.Contains(content, "hunter2") {
		t.Errorf("view.Content = %q, want the password not to appear in it", content)
	}
}

// Enter returns the login call as a command rather than making it inside
// Update. Update runs on bubbletea's one event loop, so a blocking call there
// freezes the whole program for up to the client's ten-second timeout.
func TestLoginScreenSubmitsFromACommandNotFromUpdate(t *testing.T) {
	client, calls := loginServer(t, http.StatusOK, "application/json",
		`{"token":"jwt","expires_in":3600}`)
	s := filledLoginScreen(t, client)

	_, cmd := pressLogin(t, s, enterKey)
	if got := calls.Load(); got != 0 {
		t.Fatalf("requests after Update = %d, want 0: the call belongs in the returned command", got)
	}
	if cmd == nil {
		t.Fatal("Update on enter returned a nil command, want the submit command")
	}

	msg := cmd()
	if got := calls.Load(); got != 1 {
		t.Errorf("requests after running the command = %d, want 1", got)
	}
	loggedIn, ok := msg.(loggedInMsg)
	if !ok {
		t.Fatalf("submit command returned %T, want loggedInMsg", msg)
	}
	if got, want := loggedIn.Token, "jwt"; got != want {
		t.Errorf("loggedInMsg.Token = %q, want %q", got, want)
	}
}

// A rejected login is the case the errMsg comment in root.go carves out: the
// user can fix it from this screen, so it must reach the login screen and be
// shown there rather than being caught by root as a fatal errMsg. Driven
// through root, because root's switch sees every message first.
func TestLoginFailureIsShownOnTheLoginScreenNotByRoot(t *testing.T) {
	client, _ := loginServer(t, http.StatusUnauthorized, "application/problem+json",
		`{"title":"Unauthorized","status":401,"detail":"invalid credentials"}`)
	m := root{
		ctx:    context.Background(),
		client: client,
		screen: filledLoginScreen(t, client),
	}

	next, cmd := m.Update(enterKey)
	m = next.(root)
	if cmd == nil {
		t.Fatal("root.Update on enter returned a nil command, want the submit command")
	}
	m = update(t, m, cmd())

	if m.err != nil {
		t.Fatalf("root.err = %v, want nil: a rejected login must not end the program", m.err)
	}
	if _, ok := m.screen.(loginScreen); !ok {
		t.Fatalf("screen after a rejected login = %T, want loginScreen", m.screen)
	}
	if content := ansi.Strip(m.View().Content); !strings.Contains(content, "invalid credentials") {
		t.Errorf("view.Content = %q, want it to contain the problem detail %q",
			content, "invalid credentials")
	}
}
