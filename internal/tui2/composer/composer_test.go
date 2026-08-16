package composer

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func TestNewDefaults(t *testing.T) {
	m := New(Options{})
	if m.sendKey != defaultSendKey {
		t.Fatalf("sendKey = %q, want %q", m.sendKey, defaultSendKey)
	}
	if len(m.newlineKeys) != 1 || m.newlineKeys[0] != defaultNewlineKey {
		t.Fatalf("newlineKeys = %v, want [%q]", m.newlineKeys, defaultNewlineKey)
	}
	if m.Value() != "" {
		t.Fatalf("Value() = %q, want empty", m.Value())
	}
	// A zero-value composer must render and accept keys without panicking —
	// OnSubmit and Styler are both nil here.
	m.Render(40, 3)
	m.Key(enterKey())
}

func TestNewHonorsGivenOptions(t *testing.T) {
	var got string
	m := New(Options{
		OnSubmit:    func(s string) { got = s },
		SendKey:     "enter",
		NewlineKeys: []string{"shift+enter", "alt+enter"},
	})
	m.Key(charKey('h'))
	m.Key(charKey('i'))
	m.Key(enterKey())
	if got != "hi" {
		t.Fatalf("OnSubmit got %q, want %q", got, "hi")
	}
}

func TestNewCopiesNewlineKeys(t *testing.T) {
	given := []string{"alt+enter"}
	m := New(Options{NewlineKeys: given})
	given[0] = "mutated"
	if m.newlineKeys[0] != "alt+enter" {
		t.Fatalf("New must not alias the caller's NewlineKeys slice, got %v", m.newlineKeys)
	}
}

func TestFocusTogglesStylerFocus(t *testing.T) {
	m := New(Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	m.Focus(true)
	if s := m.activeStyler(); s.Focus() != tokens.FocusNormal {
		t.Fatalf("focused composer painted with %v, want FocusNormal", s.Focus())
	}
	m.Focus(false)
	if s := m.activeStyler(); s.Focus() != tokens.FocusDimmed {
		t.Fatalf("unfocused composer painted with %v, want FocusDimmed", s.Focus())
	}
}

func TestActiveStylerNilIsSafe(t *testing.T) {
	m := New(Options{})
	if m.activeStyler() != nil {
		t.Fatalf("activeStyler on a nil base styler should stay nil")
	}
	m.Focus(true)
	out := m.Render(20, 3)
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("nil styler must never emit an escape sequence, got %q", out)
	}
}

func TestValueReflectsEdits(t *testing.T) {
	m := New(Options{})
	m.Key(charKey('a'))
	m.Key(charKey('b'))
	if m.Value() != "ab" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "ab")
	}
}
