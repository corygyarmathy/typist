package main

import (
	"strings"
	"testing"
	"time"
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

// typingModel builds a model parked in stateTyping with the cursor advanced to
// the given offset by pressing the correct key at each position.
func typingModel(t *testing.T, words []string, width, cursor int) model {
	t.Helper()

	acc := newAccumulator(words, time.Now())
	for i := 0; i < cursor; i++ {
		acc.Press(acc.text[i], time.Now())
	}
	if acc.cursor != cursor {
		t.Fatalf("accumulator cursor = %d, want %d", acc.cursor, cursor)
	}

	return model{state: stateTyping, acc: acc, width: width}
}

// "the cat sat" at width 7 wraps to "the " / "cat sat", so the caret's frame
// row is its wrapped line plus however many rows the view renders above the
// text. A hardcoded row would pass only while the cursor sits on line 0.
func TestViewCursorFollowsTheWrappedLine(t *testing.T) {
	words := []string{"the", "cat", "sat"}

	tests := []struct {
		name   string
		cursor int
		wantX  int
		wantY  int
	}{
		{"first character of the first line", 0, 0, 1},
		{"the space that ends the first line", 3, 3, 1},
		{"first character of the second line", 4, 0, 2},
		{"last character of the second line", 10, 6, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := typingModel(t, words, 7, tt.cursor).View()

			if view.Cursor == nil {
				t.Fatal("view.Cursor = nil, want a cursor while typing")
			}
			if view.Cursor.X != tt.wantX || view.Cursor.Y != tt.wantY {
				t.Errorf("view.Cursor = (%d, %d), want (%d, %d)",
					view.Cursor.X, view.Cursor.Y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestViewRendersEveryWrappedLine(t *testing.T) {
	view := typingModel(t, []string{"the", "cat", "sat"}, 7, 0).View()

	if !strings.Contains(view.Content, "the \ncat sat") {
		t.Errorf("view.Content = %q, want it to contain the wrapped text %q", view.Content, "the \ncat sat")
	}
}
