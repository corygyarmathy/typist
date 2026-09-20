package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

type state int

const (
	stateLoading state = iota
	stateTyping
	stateDone
)

var (
	typedStyle     = lipgloss.NewStyle().Faint(true)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Underline(true)
	remainderStyle = lipgloss.NewStyle()
	rejectStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Reverse(true)
)

type lessonMsg openapi.Lesson
type summaryMsg openapi.SessionSummary
type errMsg error

type model struct {
	state    state
	client   *Client
	ctx      context.Context // stored here b/c tea.Cmd() can't take it
	lesson   openapi.Lesson
	summary  *openapi.SessionSummary
	acc      *accumulator
	width    int
	rejected rune
	err      error
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
			m.rejected = 0
			if !m.acc.Press(msg.Code, time.Now()) {
				m.rejected = msg.Code
			}
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

	case tea.WindowSizeMsg:
		m.width = msg.Width

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
	var cursor *tea.Cursor

	switch m.state {
	case stateLoading:
		// m.acc is nil until lessonMsg arrives, so nothing below may run yet.
		s = "Loading lesson...\n"

	case stateTyping:
		header := "\n"
		text, starts := m.renderText()
		s = header + text

		s += fmt.Sprintf("%d/%d chars, %d errors", m.acc.cursor, len(m.acc.text), m.acc.Errors())
		if m.rejected != 0 {
			s += "  " + rejectStyle.Render(fmt.Sprintf("wrong key: %q", m.rejected))
		}
		s += "\n"

		// The index of the last start at or before the cursor - the wrapped
		// line the cursor sits on. tea.Cursor.Position is relative to the
		// frame, not the text, so the rows rendered above the text shift it
		// down; deriving that from the header keeps the two in step.
		lineNum := sort.SearchInts(starts, m.acc.cursor+1) - 1
		y := strings.Count(header, "\n") + lineNum
		cursor = tea.NewCursor(m.acc.cursor-starts[lineNum], y)

	case stateDone:
		text, _ := m.renderText()
		s = text + "\n"
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
	view := tea.NewView(s)
	view.Cursor = cursor
	return view
}

// lineStarts returns the rune index at which each wrapped line of text begins,
// greedily breaking after the last space that fits in a line of width cells.
// Line k is text[starts[k]:starts[k+1]], and the last line runs to the end of
// text, so the lines partition the input: no rune is dropped or repeated. That
// is what makes a caret position derived from a start trustworthy. The result
// always holds at least one element, 0, so callers never check for empty.
//
// Breaking after the space keeps it on the line it terminates, because it is a
// character the typist still has to press and so needs a cell of its own. A
// word longer than width is broken at width rather than overflowing.
//
// One rune is treated as one terminal cell. The corpus is ASCII
// (internal/corpus/data/corpus.json), so no grapheme or width handling is
// needed here; text outside that range would need it.
func lineStarts(text []rune, width int) []int {
	// Also the loop's termination guarantee: with width >= 1, the space branch
	// advances by at least 1 and the break-inside-a-word branch by width.
	if width <= 0 {
		return []int{0}
	}

	starts := []int{0}

	for i := 0; i < len(text)-width; {
		window := text[i : i+width]
		lastSpace := -1
		for j, r := range window {
			if r == ' ' {
				lastSpace = j
			}
		}
		if lastSpace < 0 {
			// word longer than width, break at width
			i = i + width
			starts = append(starts, i)
		} else {
			// break after last space
			i = i + lastSpace + 1
			starts = append(starts, i)
		}
	}

	return starts
}

// renderText wraps the lesson text to the current width and styles every
// position in it, one trailing newline per line. The line starts come back
// with it because the caret has to be placed from the same starts the text was
// drawn from; recomputing them separately is how the two drift apart.
func (m model) renderText() (string, []int) {
	starts := lineStarts(m.acc.text, m.width)

	var b strings.Builder
	for k, start := range starts {
		end := len(m.acc.text) // the last line runs to the end of the text
		if k+1 < len(starts) { // every other line ends where the next begins
			end = starts[k+1]
		}
		b.WriteString(m.renderLine(m.acc.text[start:end], start))
		b.WriteString("\n")
	}

	return b.String(), starts
}

// renderLine styles one wrapped line. offset is the line's rune index into
// m.acc.text, so the position of line[i] is offset+i.
func (m model) renderLine(line []rune, offset int) string {
	var b strings.Builder
	for i, r := range line {
		b.WriteString(m.styleFor(offset + i).Render(string(r)))
	}
	return b.String()
}

// styleFor returns the style for the position at index i in m.acc.text. The
// case order is the precedence. A rejection is transient - the next keypress
// replaces or clears it - so it outranks the permanent mark. firstTryError is
// the record the submission is built from, so it stays visible after the
// position is typed correctly, and outranks the typed/untyped split.
func (m model) styleFor(i int) lipgloss.Style {
	switch {
	case m.rejected != 0 && i == m.acc.cursor:
		return rejectStyle
	case m.acc.positions[i].firstTryError:
		return errorStyle
	case i < m.acc.cursor:
		return typedStyle
	default:
		return remainderStyle
	}
}
