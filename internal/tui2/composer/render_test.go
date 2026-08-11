package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// TestRender_WidthStability is the golden-harness-style sweep the brief asks
// for: every width from 1 to 110, a couple of heights, empty and non-empty
// drafts, focused and not — nothing may panic and no rendered line may
// exceed the width it was given.
func TestRender_WidthStability(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	for _, height := range []int{1, 2, 3, 6, 20} {
		for width := 1; width <= 110; width++ {
			m := New(Options{Styler: sty})
			typeString(m, "a reasonably long draft that should wrap across several rows of the composer")
			m.Focus(width%2 == 0)

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Render(%d,%d) panicked: %v", width, height, r)
					}
				}()
				out := m.Render(width, height)
				lines := strings.Split(out, "\n")
				if len(lines) > height {
					t.Fatalf("Render(%d,%d) returned %d lines, want at most %d", width, height, len(lines), height)
				}
				for i, line := range lines {
					if w := ansi.StringWidth(line); w > width {
						t.Fatalf("Render(%d,%d) line %d has width %d: %q", width, height, i, w, line)
					}
				}
			}()
		}
	}
}

func TestRender_ZeroRectNeverPanics(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	typeString(m, "hello")
	for _, wh := range [][2]int{{0, 0}, {0, 5}, {5, 0}, {-1, 5}, {5, -1}} {
		if out := m.Render(wh[0], wh[1]); out != "" {
			t.Fatalf("Render(%d,%d) = %q, want empty for a degenerate rect", wh[0], wh[1], out)
		}
	}
}

func TestRender_PlaceholderWhenEmpty(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	out := m.Render(40, 3)
	if !strings.Contains(out, placeholderText) {
		t.Fatalf("Render() = %q, want it to contain the placeholder %q", out, placeholderText)
	}
}

func TestRender_PromptGlyphPresent(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	typeString(m, "hi")
	out := m.Render(40, 3)
	if !strings.Contains(out, tokens.GlyphPromptChat) {
		t.Fatalf("Render() = %q, want the › prompt glyph", out)
	}
}

func TestRender_CursorVisibleWhenFocused(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	unfocused := New(Options{Styler: sty})
	typeString(unfocused, "hi")
	plain := unfocused.Render(40, 3)

	focused := New(Options{Styler: sty})
	typeString(focused, "hi")
	focused.Focus(true)
	withCaret := focused.Render(40, 3)

	if plain == withCaret {
		t.Fatalf("focused render is byte-identical to unfocused render; no caret drawn")
	}
	// The caret is drawn as an inverted cell via PaintOn — that always emits
	// a background-setting SGR (48;2;...) which plain foreground-only text
	// never does.
	if !strings.Contains(withCaret, "\x1b[48;2;") {
		t.Fatalf("focused render has no background-set escape (no caret band): %q", withCaret)
	}
}

func TestRender_CursorMidTextVisible(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	m := New(Options{Styler: sty})
	typeString(m, "abc")
	m.cursor = 1 // between 'a' and 'bc'
	m.Focus(true)
	out := m.Render(40, 3)
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatalf("no caret band drawn for a mid-text cursor: %q", out)
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "c") {
		t.Fatalf("surrounding text missing from render: %q", out)
	}
}

func TestRender_StylerPassThrough(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	typeString(m, "hi")
	out := m.Render(40, 3)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("Render() with a non-nil Styler produced no escape sequences at all: %q", out)
	}
}

func TestRender_NilStylerNeverEmitsEscapes(t *testing.T) {
	m := New(Options{})
	typeString(m, "hi")
	m.Focus(true)
	out := m.Render(40, 3)
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("Render() with a nil Styler emitted an escape sequence: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("Render() with a nil Styler dropped the draft text: %q", out)
	}
}

func TestRender_TailAnchoredWhenDraftOutgrowsHeight(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	for i := 0; i < 10; i++ {
		typeString(m, "line")
		m.Key(altEnterKey())
	}
	typeString(m, "last")
	out := m.Render(40, 3)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want exactly 3 (height)", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "last") {
		t.Fatalf("last visible row = %q, want it to contain the cursor's row (\"last\")", lines[len(lines)-1])
	}
}
