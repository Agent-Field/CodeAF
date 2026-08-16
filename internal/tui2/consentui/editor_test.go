package consentui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
)

func TestEditorEditsWhereTheCursorIs(t *testing.T) {
	var e editor
	e.insert("hello world")
	e.home()
	e.right()
	e.insert("E")
	if got := e.String(); got != "hEello world" {
		t.Fatalf("insert at cursor = %q", got)
	}
	e.backspace()
	if got := e.String(); got != "hello world" {
		t.Fatalf("backspace = %q", got)
	}
	e.end()
	e.left()
	e.deleteForward()
	if got := e.String(); got != "hello worl" {
		t.Fatalf("delete forward = %q", got)
	}
	e.home()
	e.key("ctrl+u", "")
	if got := e.String(); got != "hello worl" {
		t.Fatalf("ctrl+u at column 0 removed something: %q", got)
	}
	e.end()
	e.key("ctrl+u", "")
	if got := e.String(); got != "" {
		t.Fatalf("ctrl+u at end = %q, want empty", got)
	}
}

func TestEditorSurvivesMultiByteAndWideRunes(t *testing.T) {
	var e editor
	e.insert("héllo 世界")
	e.left()
	e.insert("!")
	if got := e.String(); got != "héllo 世!界" {
		t.Fatalf("wide-rune edit = %q", got)
	}
	e.backspace()
	e.backspace()
	if got := e.String(); got != "héllo 界" {
		t.Fatalf("backspace over a wide rune = %q", got)
	}
}

func TestEditorDropsControlCharactersAndFlattensNewlines(t *testing.T) {
	var e editor
	e.insert("a\x07b\tc\nd")
	if got := e.String(); got != "ab c d" {
		t.Fatalf("insert = %q, want the control char dropped and the breaks flattened", got)
	}
}

func TestEditorKeepsTheCaretInsideTheField(t *testing.T) {
	var e editor
	e.insert(strings.Repeat("x", 200))
	for _, width := range []int{0, 1, 2, 5, 20, 80} {
		row := e.caretRow(width)
		if got := blocks.Width(row); got > width && width > 0 {
			t.Fatalf("width %d: caret row is %d cells (%q)", width, got, row)
		}
		if width > 1 && !strings.Contains(row, caret) {
			t.Fatalf("width %d: the caret is off screen (%q)", width, row)
		}
	}
	// The window follows the cursor back to the head of the line.
	e.home()
	row := e.caretRow(20)
	if !strings.HasPrefix(row, caret) {
		t.Fatalf("home did not bring the caret into view: %q", row)
	}
}

func TestEditorIgnoresKeysItDoesNotOwn(t *testing.T) {
	var e editor
	if e.key("enter", "") {
		t.Fatalf("the editor swallowed enter")
	}
	if e.key("esc", "") {
		t.Fatalf("the editor swallowed esc — that is how a draft gets destroyed")
	}
}

func TestScopeSelectionWraps(t *testing.T) {
	h := newHarness(t)
	h.model.Push(scopedQuestion())
	press(t, h.model, "2")
	if h.model.scopeSel != 0 {
		t.Fatalf("scope selection started at %d", h.model.scopeSel)
	}
	press(t, h.model, "down")
	if h.model.scopeSel != 1 {
		t.Fatalf("down = %d", h.model.scopeSel)
	}
	press(t, h.model, "down")
	if h.model.scopeSel != 0 {
		t.Fatalf("the scope selection did not wrap: %d", h.model.scopeSel)
	}
	press(t, h.model, "up")
	if h.model.scopeSel != 1 {
		t.Fatalf("up did not wrap backwards: %d", h.model.scopeSel)
	}
}

func TestClearingAPatternRemovesItRatherThanKeepingIt(t *testing.T) {
	h := newHarness(t)
	h.model.Push(scopedQuestion())
	press(t, h.model, "2")
	press(t, h.model, "e")
	for i := 0; i < 40; i++ {
		press(t, h.model, "backspace")
	}
	press(t, h.model, "enter")
	if len(h.model.scope) != 1 || h.model.scope[0] != "bash(git diff:*)" {
		t.Fatalf("clearing a pattern did not narrow the grant: %v", h.model.scope)
	}
	press(t, h.model, "enter")
	if len(h.results) != 1 || len(h.results[0].Scope) != 1 {
		t.Fatalf("the granted scope is not the narrowed one: %+v", h.results)
	}
}

func TestOptionSelectionWraps(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	if h.model.sel != 0 {
		t.Fatalf("selection did not start on the durable default: %d", h.model.sel)
	}
	press(t, h.model, "up")
	if h.model.sel != 1 {
		t.Fatalf("up did not wrap to the last answer: %d", h.model.sel)
	}
	press(t, h.model, "down")
	if h.model.sel != 0 {
		t.Fatalf("down did not wrap back: %d", h.model.sel)
	}
}
