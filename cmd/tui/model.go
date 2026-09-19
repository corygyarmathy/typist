package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

type state int

const (
	stateLoading state = iota
	stateTyping
	stateDone
)

type lessonMsg openapi.Lesson
type summaryMsg openapi.SessionSummary
type errMsg error

type model struct {
	state   state
	client  *Client
	ctx     context.Context // stored here b/c tea.Cmd() can't take it
	lesson  openapi.Lesson
	summary *openapi.SessionSummary
	acc     *accumulator
	err     error
}

func initialModel(ctx context.Context, client *Client) model {
	return model{
		client: client,
		ctx:    ctx,
	}
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		lesson, err := m.client.NextLesson(m.ctx)
		if err != nil {
			return errMsg(fmt.Errorf("getting next lesson: %w", err))
		}
		return lessonMsg(lesson)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// Is it a key press?
	case tea.KeyPressMsg:

		// What was the actual key pressed?
		switch msg.String() {

		// These keys should exit the program.
		case "ctrl+c":
			return m, tea.Quit
		default:
			// lesson hasn't finished loading
			if m.state != stateTyping {
				break
			}
			// empty means a non-printable key - arrows, shift, function keys
			if msg.Text == "" {
				break
			}
			m.acc.Press(msg.Code, time.Now())
			if m.acc.Done() {
				m.state = stateDone
				sub := m.acc.Submission()
				return m, func() tea.Msg {
					summary, err := m.client.SubmitSession(m.ctx, sub)
					if err != nil {
						return errMsg(fmt.Errorf("submitting session: %w", err))
					}
					return summaryMsg(summary)
				}
			}
		}

	case lessonMsg:
		m.state = stateTyping
		m.lesson = openapi.Lesson(msg)
		m.acc = newAccumulator(m.lesson.Words, time.Now())
		return m, nil

	case summaryMsg:
		summary := openapi.SessionSummary(msg)
		m.summary = &summary
		return m, nil

	case errMsg:
		m.err = msg
		return m, nil
	}

	// Return the updated model to the Bubble Tea runtime for processing.
	// Note that we're not returning a command.
	return m, nil
}

func (m model) View() tea.View {
	if m.err != nil {
		return tea.NewView("Error: " + m.err.Error() + "\n")
	}

	var s string
	switch m.state {
	case stateLoading:
		// m.acc is nil until lessonMsg arrives, so nothing below may run yet.
		s = "Loading lesson...\n"

	case stateTyping:
		// The intended text on one line, a caret beneath the character due
		// next. Slicing at m.acc.cursor is valid at every value including
		// len(text); indexing a single character at len(text) would panic,
		// so the caret line avoids indexing the text at all.
		s = string(m.acc.text) + "\n"
		s += strings.Repeat(" ", m.acc.cursor) + "^\n\n"
		s += fmt.Sprintf(
			"%d/%d chars, %d errors\n",
			m.acc.cursor, len(m.acc.text), m.acc.Errors(),
		)

	case stateDone:
		s = string(m.acc.text) + "\n\n"
		if m.summary == nil {
			s += "Submitting...\n"
			break
		}
		s += fmt.Sprintf(
			"%d wpm, %.1f%% accuracy\n",
			m.summary.Wpm, m.summary.Accuracy*100,
		)
	}

	s += "\nPress ctrl+c to quit.\n"

	// Send the UI for rendering
	return tea.NewView(s)
}
