package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// doneModel builds a results screen for a lesson typed to its end.
func doneModel(t *testing.T, words []string, width int) resultsScreen {
	t.Helper()

	acc := newAccumulator(words, time.Now())
	for !acc.Done() {
		acc.Press(acc.text[acc.cursor], time.Now())
	}

	return resultsScreen{lessonText: lessonText{acc: acc, width: width}}
}

// The results screen shows the same text the typing screen did, so it has to
// wrap the same way. bubbletea's renderer truncates a line wider than the
// frame rather than soft-wrapping it, so an unwrapped results screen loses
// every line but the first - the text does not merely reflow, it disappears.
func TestViewWrapsTheTextOnTheResultsScreen(t *testing.T) {
	view := doneModel(t, []string{"the", "cat", "sat"}, 7).View()

	if !strings.Contains(ansi.Strip(view.Content), "the \ncat sat") {
		t.Errorf("view.Content = %q, want its stripped text to contain the wrapped lesson %q",
			view.Content, "the \ncat sat")
	}
}
