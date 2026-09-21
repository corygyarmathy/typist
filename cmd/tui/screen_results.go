package main

// The results screen: the lesson as it was typed, and what the server made of
// it.

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

// summaryMsg carries the server's scoring of the submitted session.
type summaryMsg openapi.SessionSummary

type resultsScreen struct {
	lessonText
	ctx     context.Context // stored here b/c tea.Cmd() can't take it
	client  *Client
	summary *openapi.SessionSummary
}

func newResultsScreen(ctx context.Context, client *Client, width int, acc *accumulator) resultsScreen {
	return resultsScreen{
		lessonText: lessonText{acc: acc, width: width},
		ctx:        ctx,
		client:     client,
	}
}

// Init submits the session, for the same reason the loading screen owns its
// fetch: the screen that displays a result issues the request for it.
func (s resultsScreen) Init() tea.Cmd {
	sub := s.acc.Submission()
	return func() tea.Msg {
		summary, err := s.client.SubmitSession(s.ctx, sub)
		if err != nil {
			return errMsg(fmt.Errorf("submitting session: %w", err))
		}
		return summaryMsg(summary)
	}
}

// Update takes summaryMsg itself rather than leaving it to root, unlike
// lessonMsg and finishedMsg: it fills this screen in rather than replacing it,
// so it is not a transition and root has no business in it.
func (s resultsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		s.width = msg.Width

	case summaryMsg:
		summary := openapi.SessionSummary(msg)
		s.summary = &summary
	}

	return s, nil
}

func (s resultsScreen) View() tea.View {
	text, _ := s.render()

	out := text + "\n"
	if s.summary == nil {
		out += "Submitting...\n"
	} else {
		out += fmt.Sprintf(
			"%d wpm, %.1f%% accuracy\n",
			s.summary.Wpm, s.summary.Accuracy*100,
		)
	}

	return tea.NewView(out)
}
