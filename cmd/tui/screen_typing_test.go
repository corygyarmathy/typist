package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// typingModel builds a typing screen with the cursor advanced to the given
// offset, fumbling the listed positions on the way. The accumulator is built
// by typedTo, so the screen's state is reached the way a real session reaches
// it rather than written from outside.
func typingModel(t *testing.T, words []string, width, cursor int, fumble ...int) typingScreen {
	t.Helper()

	return typingScreen{lessonText{acc: typedTo(t, words, cursor, fumble...), width: width}}
}

// press sends one printable keystroke through Update and returns the updated
// screen, so the tests exercise the same path the runtime takes.
func press(t *testing.T, s typingScreen, r rune) typingScreen {
	t.Helper()

	next, _ := s.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	updated, ok := next.(typingScreen)
	if !ok {
		t.Fatalf("Update returned %T, want typingScreen", next)
	}
	return updated
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

// The wrapped text has to survive styling: renderLine puts ANSI escapes
// between every character, so the assertion reads the content with them
// stripped. The cursor is deliberately past 0 - at 0 every position is
// remainderStyle, which renders plain, and the test would pass without the
// styling ever running.
func TestViewRendersEveryWrappedLine(t *testing.T) {
	view := typingModel(t, []string{"the", "cat", "sat"}, 7, 5).View()

	if !strings.Contains(ansi.Strip(view.Content), "the \ncat sat") {
		t.Errorf("view.Content = %q, want its stripped text to contain %q", view.Content, "the \ncat sat")
	}
}

// A second wrong key replaces the first rather than leaving the earlier one on
// screen, which is the rule the unconditional clear in Update encodes.
func TestASecondWrongKeystrokeReplacesTheRejection(t *testing.T) {
	m := press(t, press(t, typingModel(t, []string{"the", "cat", "sat"}, 7, 4), 'z'), 'q')

	if m.rejected != 'q' {
		t.Errorf("typingScreen.rejected = %q, want %q - the most recent wrong key", m.rejected, 'q')
	}
}

// The other half of the transient/permanent rule asserted in
// lessontext_test.go: a correct keystroke clears the rejection. Update clears
// it unconditionally before pressing, so this is the rule, not an edge case.
func TestACorrectKeystrokeClearsTheRejection(t *testing.T) {
	m := press(t, press(t, typingModel(t, []string{"the", "cat", "sat"}, 7, 4), 'z'), 'c')

	if m.rejected != 0 {
		t.Errorf("typingScreen.rejected = %q, want it cleared by a correct keystroke", m.rejected)
	}
}

// Naming the rejected key is the whole reason Press returns a bool; the
// position's style alone says a key was wrong but never which one.
func TestViewNamesTheRejectedKey(t *testing.T) {
	view := press(t, typingModel(t, []string{"the", "cat", "sat"}, 7, 4), 'z').View()

	if want := `wrong key: 'z'`; !strings.Contains(ansi.Strip(view.Content), want) {
		t.Errorf("view.Content = %q, want its stripped text to contain %q", view.Content, want)
	}
}

func TestViewOmitsTheRejectedKeyWhenThereIsNone(t *testing.T) {
	view := typingModel(t, []string{"the", "cat", "sat"}, 7, 4).View()

	if strings.Contains(ansi.Strip(view.Content), "wrong key") {
		t.Errorf("view.Content = %q, want no rejection notice before a wrong keystroke", view.Content)
	}
}
