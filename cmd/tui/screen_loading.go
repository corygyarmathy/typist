package main

// The loading screen: the one row shown while the next lesson is fetched.

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

// lessonMsg reports the lesson this screen was waiting for. It is addressed to
// root, which is what swaps this screen for the typing one.
type lessonMsg openapi.Lesson

// loadingScreen handles no input, which is the argument for folding it into
// the typing screen as a flag. It stays a screen of its own because it owns
// the command that ends it: the fetch and the state that waits on the fetch
// are the same thing, and a `loading bool` on the typing screen would put them
// in different places while giving the typing screen back the mode field the
// split just deleted.
type loadingScreen struct {
	ctx    context.Context // stored here b/c tea.Cmd() can't take it
	client *Client
}

func newLoadingScreen(ctx context.Context, client *Client) loadingScreen {
	return loadingScreen{ctx: ctx, client: client}
}

func (s loadingScreen) Init() tea.Cmd {
	return func() tea.Msg {
		lesson, err := s.client.NextLesson(s.ctx)
		if err != nil {
			return errMsg(fmt.Errorf("getting next lesson: %w", err))
		}
		return lessonMsg(lesson)
	}
}

// Update ignores everything. Both messages that matter here - the lesson and a
// failure to fetch it - are root's to act on, and a screen that cannot be
// interacted with has no third case.
func (s loadingScreen) Update(tea.Msg) (screen, tea.Cmd) { return s, nil }

func (s loadingScreen) View() tea.View { return tea.NewView("Loading lesson...\n") }
