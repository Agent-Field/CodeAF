package composer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pastePane pins the shape doc.go promises the shell wiring: whatever the
// eventual tui2 PanePaste interface looks like, it is Paste(tea.PasteMsg)
// tea.Cmd — the same pattern as PaneKeys.Key and PaneMouse.Mouse. If this
// package's Paste method ever drifts from that shape, this assertion fails
// the build rather than the drift surfacing as a silent no-op in the shell.
type pastePane interface {
	Paste(tea.PasteMsg) tea.Cmd
}

var _ pastePane = (*Model)(nil)

func TestPaste_NeverSends(t *testing.T) {
	var calls int
	m := New(Options{OnSubmit: func(string) { calls++ }})
	m.Paste(tea.PasteMsg{Content: "line one\nline two\nline three\n"})
	if calls != 0 {
		t.Fatalf("OnSubmit called %d times from a multi-line paste, want 0", calls)
	}
	if m.Value() != "line one\nline two\nline three\n" {
		t.Fatalf("Value() = %q, want the pasted content inserted verbatim", m.Value())
	}
}

func TestPaste_InsertsAtCursor(t *testing.T) {
	m := New(Options{})
	typeString(m, "ac")
	m.Key(leftKey())
	m.Paste(tea.PasteMsg{Content: "B"})
	if m.Value() != "aBc" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "aBc")
	}
}

func TestPaste_NormalizesLineEndings(t *testing.T) {
	m := New(Options{})
	m.Paste(tea.PasteMsg{Content: "a\r\nb\rc"})
	if m.Value() != "a\nb\nc" {
		t.Fatalf("Value() = %q, want CRLF/CR normalized to LF", m.Value())
	}
}

func TestPaste_ThenSendRequiresExplicitSendKey(t *testing.T) {
	var sent string
	m := New(Options{OnSubmit: func(s string) { sent = s }})
	m.Paste(tea.PasteMsg{Content: "pasted text\n"})
	if sent != "" {
		t.Fatalf("paste alone must never call OnSubmit, got %q", sent)
	}
	m.Key(enterKey())
	if sent != "pasted text" {
		t.Fatalf("OnSubmit got %q after an explicit send, want the trimmed paste", sent)
	}
}

func TestPaste_LargeMultilineNeverPanicsRender(t *testing.T) {
	m := New(Options{})
	m.Paste(tea.PasteMsg{Content: strings.Repeat("a paragraph of pasted prose\n", 50)})
	m.Render(30, 3)
}
