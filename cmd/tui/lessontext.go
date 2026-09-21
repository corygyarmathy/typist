package main

// The lesson text as both screens that draw it need it: wrapped to the
// terminal width and styled per position.

import (
	"sort"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

var (
	typedStyle     = lipgloss.NewStyle().Faint(true)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Underline(true)
	remainderStyle = lipgloss.NewStyle()
	rejectStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Reverse(true)
)

// lessonText draws the lesson. It is embedded by the typing screen and the
// results screen, which are the two screens that show the text and the only
// two that ever will; the fields below are exactly what drawing it needs and
// nothing else. The results screen leaves rejected at 0, because a rejection
// is a thing the typist is being told about a keystroke they can still fix.
type lessonText struct {
	acc      *accumulator
	width    int
	rejected rune
}

// render wraps the text to the current width and styles every position in it,
// one trailing newline per line. The line starts come back with it because the
// caret has to be placed from the same starts the text was drawn from;
// recomputing them separately is how the two drift apart.
func (l lessonText) render() (string, []int) {
	starts := lineStarts(l.acc.text, l.width)

	var b strings.Builder
	for k, start := range starts {
		end := len(l.acc.text) // the last line runs to the end of the text
		if k+1 < len(starts) { // every other line ends where the next begins
			end = starts[k+1]
		}
		b.WriteString(l.renderLine(l.acc.text[start:end], start))
		b.WriteString("\n")
	}

	return b.String(), starts
}

// cursorAt returns the caret's column and its row within the rendered text,
// given the line starts render returned. The row is the last start at or
// before the cursor and the column is the distance from it - the arithmetic
// the starts exist for. The caller adds however many rows it drew above the
// text, because tea.Cursor.Position is relative to the frame.
func (l lessonText) cursorAt(starts []int) (x, y int) {
	line := sort.SearchInts(starts, l.acc.cursor+1) - 1
	return l.acc.cursor - starts[line], line
}

// renderLine styles one wrapped line. offset is the line's rune index into
// l.acc.text, so the position of line[i] is offset+i.
func (l lessonText) renderLine(line []rune, offset int) string {
	var b strings.Builder
	for i, r := range line {
		b.WriteString(l.styleFor(offset + i).Render(string(r)))
	}
	return b.String()
}

// styleFor returns the style for the position at index i in l.acc.text. The
// case order is the precedence. A rejection is transient - the next keypress
// replaces or clears it - so it outranks the permanent mark. firstTryError is
// the record the submission is built from, so it stays visible after the
// position is typed correctly, and outranks the typed/untyped split.
func (l lessonText) styleFor(i int) lipgloss.Style {
	switch {
	case l.rejected != 0 && i == l.acc.cursor:
		return rejectStyle
	case l.acc.positions[i].firstTryError:
		return errorStyle
	case i < l.acc.cursor:
		return typedStyle
	default:
		return remainderStyle
	}
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
