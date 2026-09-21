package main

// The typing screen: the lesson, the caret, and the keystrokes.

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

// finishedMsg reports that the lesson was typed to its end and carries the
// accumulator that recorded it. The accumulator travels rather than the
// submission it can build, because the results screen shows the text as well
// as the score.
type finishedMsg struct{ acc *accumulator }

type typingScreen struct {
	lessonText
}

func newTypingScreen(width int, lesson openapi.Lesson) typingScreen {
	return typingScreen{lessonText{
		acc:   newAccumulator(lesson.Words, time.Now()),
		width: width,
	}}
}

func (s typingScreen) Init() tea.Cmd { return nil }

func (s typingScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		s.width = msg.Width

	case tea.KeyPressMsg:
		// empty means a non-printable key - arrows, shift, function keys
		if msg.Text == "" {
			break
		}
		s.rejected = 0
		if !s.acc.Press(msg.Code, time.Now()) {
			s.rejected = msg.Code
		}
		if s.acc.Done() {
			acc := s.acc
			return s, func() tea.Msg { return finishedMsg{acc} }
		}
	}

	return s, nil
}

func (s typingScreen) View() tea.View {
	header := "\n"
	text, starts := s.render()

	out := header + text
	out += fmt.Sprintf("%d/%d chars, %d errors", s.acc.cursor, len(s.acc.text), s.acc.Errors())
	if s.rejected != 0 {
		out += "  " + rejectStyle.Render(fmt.Sprintf("wrong key: %q", s.rejected))
	}
	out += "\n"

	// tea.Cursor.Position is relative to the frame, not the text, so the rows
	// rendered above the text shift it down; deriving that from the header
	// keeps the two in step.
	x, line := s.cursorAt(starts)

	view := tea.NewView(out)
	view.Cursor = tea.NewCursor(x, strings.Count(header, "\n")+line)
	return view
}
