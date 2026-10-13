package main

// The login screen: an email and a password, submitted to exchange them for a
// token.

import (
	"context"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

// loggedInMsg carries the token a successful login was issued. It is addressed
// to root, which stores the token and moves on to the loading screen.
type loggedInMsg openapi.TokenResponse

// loginFailedMsg reports the message for login failed. Implemented as struct
// to avoid type matching w/ root's ErrMsg.
type loginFailedMsg struct{ err error }

// loginScreen shows its own failures rather than sending errMsg: a wrong
// password or an unreachable server can both be retried from here with enter.
type loginScreen struct {
	ctx      context.Context // stored here b/c tea.Cmd() can't take it
	client   *Client
	email    textinput.Model
	password textinput.Model
	err      error
}

func newLoginScreen(ctx context.Context, client *Client) loginScreen {
	email := textinput.New()
	password := textinput.New()

	password.EchoMode = textinput.EchoPassword

	// The tea.Cmd Focus returns only starts the cursor blinking, so it is
	// dropped rather than threaded through to Init.
	email.Focus()

	return loginScreen{
		ctx:      ctx,
		client:   client,
		email:    email,
		password: password,
	}
}

func (s loginScreen) Init() tea.Cmd {
	return nil
}

// Update runs the login as a command rather than calling it here: Update runs
// on bubbletea's one event loop, and a blocking request would freeze the
// program for up to the client's timeout.
func (s loginScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loginFailedMsg:
		s.err = msg.err

	case tea.KeyPressMsg:
		if msg.String() == "tab" || msg.String() == "shift+tab" {
			if s.email.Focused() {
				s.password.Focus()
				s.email.Blur()
			} else {
				s.email.Focus()
				s.password.Blur()
			}
			return s, nil
		}
		if msg.String() == "enter" {
			email, password := s.email.Value(), s.password.Value()
			return s, func() tea.Msg {
				token, err := s.client.Login(s.ctx, email, password)
				if err != nil {
					return loginFailedMsg{err: err}
				}
				return loggedInMsg(token)
			}
		}
	}

	// Both fields get every other message; an unfocused textinput ignores keys,
	// so only the focused one changes.
	var emailCmd, passwordCmd tea.Cmd
	s.email, emailCmd = s.email.Update(msg)
	s.password, passwordCmd = s.password.Update(msg)
	return s, tea.Batch(emailCmd, passwordCmd)
}

func (s loginScreen) View() tea.View {
	var out string
	out += "\nEmail "
	out += s.email.View()
	out += "\nPassword "
	out += s.password.View()

	if s.err != nil {
		out += "\n\n Error: " + s.err.Error()
	}

	view := tea.NewView(out)
	return view

}
