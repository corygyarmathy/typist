package main

// The root model: it owns what every screen needs and nothing a screen can
// own itself, and it is the only place a screen is swapped for another.

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

// screen is tea.Model with one return type narrowed: Update hands back a
// screen rather than a tea.Model. Nothing here is invented - it is the
// framework's own three methods - and the narrowing is what lets root assign
// the result straight back to its field instead of type-asserting on every
// keystroke.
type screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (screen, tea.Cmd)
	View() tea.View
}

// errMsg is the one message every screen can emit and none of them handles:
// root renders the error, because a screen that displays its own failures has
// to know how to be two things at once.
type errMsg error

type root struct {
	ctx    context.Context // stored here b/c tea.Cmd() can't take it
	client *Client
	screen screen
	width  int
	err    error
}

func newRoot(ctx context.Context, client *Client) root {
	return root{
		ctx:    ctx,
		client: client,
		screen: newLoadingScreen(ctx, client),
	}
}

func (m root) Init() tea.Cmd {
	return m.screen.Init()
}

// Update handles the three things that are root's business - quitting,
// tracking the window size, and swapping screens - and forwards everything
// else to the current screen.
//
// The screen swaps sit here rather than in the screens because a screen that
// constructs its successor has to know that successor exists, which is the
// coupling the split is for. A screen announces what happened to it; root
// decides what that means.
func (m root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		// Kept as well as forwarded: tea.WindowSizeMsg arrives once at startup
		// and then only on resize, so a screen constructed later never sees
		// one and would render at a width it cannot discover.
		m.width = msg.Width

	case lessonMsg:
		next := newTypingScreen(m.width, openapi.Lesson(msg))
		m.screen = next
		return m, next.Init()

	case finishedMsg:
		next := newResultsScreen(m.ctx, m.client, m.width, msg.acc)
		m.screen = next
		return m, next.Init()

	case errMsg:
		m.err = msg
		return m, nil
	}

	next, cmd := m.screen.Update(msg)
	m.screen = next
	return m, cmd
}

// View frames whatever the current screen drew. The footer goes below the
// screen's own content so that it cannot move the caret: tea.Cursor.Position
// is relative to the frame, and only rows rendered *above* the text shift it.
func (m root) View() tea.View {
	if m.err != nil {
		return tea.NewView("Error: " + m.err.Error() + "\n")
	}

	inner := m.screen.View()
	view := tea.NewView(inner.Content + "\nPress ctrl+c to quit.\n")
	view.Cursor = inner.Cursor
	return view
}
