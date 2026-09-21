package main

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/corygyarmathy/typist/internal/openapi"
)

// testRoot builds a root with a client that is never called: every test below
// stops at the screen swap, before the new screen's Init would issue a request.
func testRoot(t *testing.T) root {
	t.Helper()
	return newRoot(context.Background(), NewClient("http://example.invalid", "token"))
}

// update runs one message through root and returns the result as a root, which
// is the one type assertion the screen interface does not remove - tea.Model is
// the framework's signature for the program's top level.
func update(t *testing.T, m root, msg tea.Msg) root {
	t.Helper()

	next, _ := m.Update(msg)
	updated, ok := next.(root)
	if !ok {
		t.Fatalf("root.Update returned %T, want root", next)
	}
	return updated
}

// The screen swaps are root's whole job, and each is driven by a message a
// screen emitted rather than by the screen constructing its successor.
func TestRootSwapsScreensOnTheTransitionMessages(t *testing.T) {
	m := testRoot(t)
	if _, ok := m.screen.(loadingScreen); !ok {
		t.Fatalf("newRoot screen = %T, want loadingScreen", m.screen)
	}

	m = update(t, m, lessonMsg(openapi.Lesson{Words: []string{"the", "cat"}}))
	typing, ok := m.screen.(typingScreen)
	if !ok {
		t.Fatalf("screen after lessonMsg = %T, want typingScreen", m.screen)
	}

	m = update(t, m, finishedMsg{typing.acc})
	if _, ok := m.screen.(resultsScreen); !ok {
		t.Fatalf("screen after finishedMsg = %T, want resultsScreen", m.screen)
	}
}

// The window size is kept as well as forwarded, because a screen constructed
// after the one tea.WindowSizeMsg at startup never sees one. The typing screen
// below is created three messages later and still has to know the width.
func TestRootPassesTheRememberedWidthToALaterScreen(t *testing.T) {
	m := update(t, testRoot(t), tea.WindowSizeMsg{Width: 7, Height: 24})
	if m.width != 7 {
		t.Fatalf("root.width = %d, want 7", m.width)
	}

	m = update(t, m, lessonMsg(openapi.Lesson{Words: []string{"the", "cat", "sat"}}))
	typing, ok := m.screen.(typingScreen)
	if !ok {
		t.Fatalf("screen after lessonMsg = %T, want typingScreen", m.screen)
	}
	if typing.width != 7 {
		t.Errorf("typingScreen.width = %d, want the remembered 7", typing.width)
	}
}

// A resize after a screen exists has to reach that screen, which is why the
// message is forwarded and not merely recorded.
func TestRootForwardsAResizeToTheCurrentScreen(t *testing.T) {
	m := update(t, testRoot(t), lessonMsg(openapi.Lesson{Words: []string{"the", "cat", "sat"}}))
	m = update(t, m, tea.WindowSizeMsg{Width: 7, Height: 24})

	typing, ok := m.screen.(typingScreen)
	if !ok {
		t.Fatalf("screen = %T, want typingScreen", m.screen)
	}
	if typing.width != 7 {
		t.Errorf("typingScreen.width = %d, want 7 after a resize", typing.width)
	}
}

// Errors are root's because a screen that renders its own failures has to be
// two things at once. Every screen emits errMsg and none of them handles it.
func TestRootRendersAnErrorFromAnyScreen(t *testing.T) {
	m := update(t, testRoot(t), errMsg(context.DeadlineExceeded))

	if m.err == nil {
		t.Fatal("root.err = nil, want the error the screen emitted")
	}
	if got := m.View().Content; got != "Error: "+context.DeadlineExceeded.Error()+"\n" {
		t.Errorf("View().Content = %q, want the error text", got)
	}
}

// The footer is appended below the screen's content so it cannot move the
// caret: tea.Cursor.Position is relative to the frame, and only rows rendered
// above the text shift it.
func TestRootFramesTheScreenWithoutMovingItsCaret(t *testing.T) {
	m := update(t, testRoot(t), tea.WindowSizeMsg{Width: 7, Height: 24})
	m = update(t, m, lessonMsg(openapi.Lesson{Words: []string{"the", "cat", "sat"}}))

	inner := m.screen.View()
	outer := m.View()

	if inner.Cursor == nil || outer.Cursor == nil {
		t.Fatal("want a cursor on both the screen's view and root's")
	}
	if *outer.Cursor != *inner.Cursor {
		t.Errorf("root cursor = %v, want the screen's %v unchanged", *outer.Cursor, *inner.Cursor)
	}
}
