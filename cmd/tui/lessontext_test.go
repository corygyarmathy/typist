package main

import (
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"
)

func TestLineStarts(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []int
	}{
		{"breaks after the last space in the window", "the cat sat", 7, []int{0, 4}},
		{"forces a break inside a word longer than width", "abcdefghij", 4, []int{0, 4, 8}},
		{"does not wrap text that already fits", "short", 80, []int{0}},
		{"treats a zero width as no wrapping", "short", 0, []int{0}},
		{"treats a negative width as no wrapping", "short", -1, []int{0}},
		{"handles empty text", "", 10, []int{0}},
		{"breaks on every space at minimal width", "a b c d e", 2, []int{0, 2, 4, 6, 8}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lineStarts([]rune(tt.text), tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("lineStarts(%q, %d) = %v, want %v", tt.text, tt.width, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("lineStarts(%q, %d) = %v, want %v", tt.text, tt.width, got, tt.want)
				}
			}
		})
	}
}

// The starts must partition the text: slicing between consecutive starts and
// concatenating the pieces reproduces the input exactly. A caret position
// derived from a start is only meaningful if no rune was dropped or repeated.
func TestLineStartsPartitionsTheText(t *testing.T) {
	texts := []string{"the cat sat", "abcdefghij", "a b c d e", "", "one two three four five"}

	for _, text := range texts {
		runes := []rune(text)
		for width := 0; width <= 12; width++ {
			starts := lineStarts(runes, width)

			if len(starts) == 0 || starts[0] != 0 {
				t.Fatalf("lineStarts(%q, %d) = %v, want a non-empty slice beginning with 0", text, width, starts)
			}

			var rebuilt []rune
			for k, start := range starts {
				if k > 0 && start <= starts[k-1] {
					t.Fatalf("lineStarts(%q, %d) = %v, want strictly increasing", text, width, starts)
				}
				end := len(runes)
				if k+1 < len(starts) {
					end = starts[k+1]
				}
				if width > 0 && end-start > width {
					t.Fatalf("lineStarts(%q, %d) = %v: line %d is %d runes, want at most %d",
						text, width, starts, k, end-start, width)
				}
				rebuilt = append(rebuilt, runes[start:end]...)
			}

			if string(rebuilt) != text {
				t.Fatalf("lineStarts(%q, %d) = %v: lines rejoin to %q", text, width, starts, string(rebuilt))
			}
		}
	}
}

// styleFor's case order is its contract, so each branch is named by the state
// that should reach it. "the cat sat" indexes as t0 h1 e2 _3 c4 a5 t6 _7 s8 a9
// t10; the model below has typed through index 4, fumbling index 1 on the way.
func TestStyleForClassifiesEachPosition(t *testing.T) {
	l := lessonText{acc: typedTo(t, []string{"the", "cat", "sat"}, 5, 1), width: 7}

	tests := []struct {
		name string
		i    int
		want lipgloss.Style
	}{
		{"typed cleanly", 0, typedStyle},
		{"typed, but fumbled on the first try", 1, errorStyle},
		{"the cursor position, not yet typed", 5, remainderStyle},
		{"beyond the cursor", 6, remainderStyle},
		{"the last position of the text", 10, remainderStyle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Styles are compared by what they render, not by identity: two
			// styles are the same to a reader exactly when their output is.
			got := l.styleFor(tt.i).Render("x")
			if want := tt.want.Render("x"); got != want {
				t.Errorf("styleFor(%d).Render(\"x\") = %q, want %q", tt.i, got, want)
			}
		})
	}
}

// A rejection always coincides with a first-try error - Press sets the mark on
// the same position it refuses to advance past - so this precedence decides
// what every rejected keystroke looks like, not some edge case.
func TestStyleForRejectionOutranksTheErrorMark(t *testing.T) {
	acc := typedTo(t, []string{"the", "cat", "sat"}, 4)
	if acc.Press('z', time.Now()) {
		t.Fatal("Press('z') = true, want a rejection at position 4")
	}
	l := lessonText{acc: acc, width: 7, rejected: 'z'}

	if !acc.positions[4].firstTryError {
		t.Fatal("positions[4].firstTryError = false, want true after a wrong keystroke")
	}

	got := l.styleFor(4).Render("c")
	if want := rejectStyle.Render("c"); got != want {
		t.Errorf("styleFor(4).Render(\"c\") = %q, want the rejection style %q", got, want)
	}
}

// The error mark is permanent: it survives the position being typed
// correctly, because the mark is what Submission counts as the position's
// error. The transient half of that rule - the rejection being cleared - is
// the typing screen's, and is asserted there.
func TestStyleForKeepsTheErrorMarkAfterTheCorrection(t *testing.T) {
	l := lessonText{acc: typedTo(t, []string{"the", "cat", "sat"}, 5, 4), width: 7}

	got := l.styleFor(4).Render("c")
	if want := errorStyle.Render("c"); got != want {
		t.Errorf("styleFor(4).Render(\"c\") = %q, want the error mark to survive: %q", got, want)
	}
}

// renderLine styles per position, and offset is how a position's index is
// recovered from a slice that does not start at 0. An offset that is ignored
// or double-counted would still produce plausible-looking output, so the
// assertion is exact rather than a Contains.
func TestRenderLineStylesEachPositionAtItsOffset(t *testing.T) {
	l := lessonText{acc: typedTo(t, []string{"the", "cat", "sat"}, 5, 1), width: 7}

	tests := []struct {
		name   string
		start  int
		end    int
		offset int
		want   string
	}{
		{
			name:  "the first wrapped line, holding the fumbled position",
			start: 0, end: 4, offset: 0,
			want: typedStyle.Render("t") + errorStyle.Render("h") +
				typedStyle.Render("e") + typedStyle.Render(" "),
		},
		{
			name:  "the second wrapped line, spanning the cursor",
			start: 4, end: 11, offset: 4,
			want: typedStyle.Render("c") + remainderStyle.Render("a") +
				remainderStyle.Render("t") + remainderStyle.Render(" ") +
				remainderStyle.Render("s") + remainderStyle.Render("a") +
				remainderStyle.Render("t"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := l.renderLine(l.acc.text[tt.start:tt.end], tt.offset)
			if got != tt.want {
				t.Errorf("renderLine(text[%d:%d], %d) = %q, want %q",
					tt.start, tt.end, tt.offset, got, tt.want)
			}
		})
	}
}

// The slice 1 constraint: do not distinguish by colour alone, so the states
// stay apart on a monochrome terminal and for a colour-blind reader. Asserting
// on the SGR parameters rather than a substring because lipgloss packs every
// attribute into one sequence - errorStyle renders as "\x1b[4;31;4m", so a
// search for the literal "\x1b[4m" would find nothing and pass for the wrong
// reason. remainderStyle is excluded deliberately: it is the unstyled
// baseline the other three are distinguished from.
func TestStylesCarryANonColourAttribute(t *testing.T) {
	// SGR 1 bold, 2 faint, 3 italic, 4 underline, 7 reverse, 9 strikethrough.
	// Everything from 30 up selects a colour.
	attributes := map[string]bool{"1": true, "2": true, "3": true, "4": true, "7": true, "9": true}

	tests := []struct {
		name  string
		style lipgloss.Style
	}{
		{"typedStyle", typedStyle},
		{"errorStyle", errorStyle},
		{"rejectStyle", rejectStyle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := tt.style.Render("x")
			params := sgrParams(t, rendered)

			for _, p := range params {
				if attributes[p] {
					return
				}
			}
			t.Errorf("%s.Render(\"x\") = %q: SGR parameters %v are all colours, "+
				"want at least one of bold, faint, italic, underline, reverse or strikethrough",
				tt.name, rendered, params)
		})
	}
}

// sgrParams returns the parameters of the first SGR sequence in s - the
// semicolon-separated numbers between "\x1b[" and the terminating "m".
func sgrParams(t *testing.T, s string) []string {
	t.Helper()

	start := strings.Index(s, "\x1b[")
	if start < 0 {
		t.Fatalf("%q contains no escape sequence", s)
	}
	body := s[start+2:]
	end := strings.Index(body, "m")
	if end < 0 {
		t.Fatalf("%q has an unterminated escape sequence", s)
	}
	return strings.Split(body[:end], ";")
}

// typedTo returns an accumulator advanced to the given cursor by pressing the
// correct key at each position. Positions listed in fumble get a wrong key
// first, so their firstTryError mark is set the way a real session sets it -
// through Press - rather than by writing to accumulator.positions from
// outside. Every test that needs a part-typed lesson builds it from here.
func typedTo(t *testing.T, words []string, cursor int, fumble ...int) *accumulator {
	t.Helper()

	fumbled := make(map[int]bool, len(fumble))
	for _, i := range fumble {
		fumbled[i] = true
	}

	acc := newAccumulator(words, time.Now())
	for i := 0; i < cursor; i++ {
		if fumbled[i] {
			acc.Press(otherThan(acc.text[i]), time.Now())
		}
		acc.Press(acc.text[i], time.Now())
	}
	if acc.cursor != cursor {
		t.Fatalf("accumulator cursor = %d, want %d", acc.cursor, cursor)
	}

	return acc
}

// otherThan returns a rune that is not r, for pressing a deliberate error.
func otherThan(r rune) rune {
	if r == 'z' {
		return 'q'
	}
	return 'z'
}
