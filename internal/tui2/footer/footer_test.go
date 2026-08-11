package footer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func styler() *tokens.Styler { return tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal) }

func sampleVerbs() []registry.Entry {
	return []registry.Entry{
		{ID: "key.node.cancel", Verb: "cancel", Key: "c"},
		{ID: "key.node.restart", Verb: "restart", Key: "r"},
		{ID: "slash.tasks", Verb: "focus tasks", Slash: "tasks"},
		{ID: "belt.revise", Verb: "revise"}, // no key, no slash: talk-only
	}
}

func fullContext() FocusContext {
	return FocusContext{
		Verbs:        sampleVerbs(),
		Input:        InputTyped,
		Hint:         "↵ send",
		KeyMode:      KeyModeAnswer,
		KeyModeCount: 3,
		Attention:    2,
		Health:       []string{"mcp: 1 down"},
		Toast:        "cancelled elsewhere",
		ScopeTail:    "wisp-parity › src",
	}
}

// TestRender_WidthStability sweeps width 1..110 (plus a couple of
// pathological values) across a spread of FocusContext shapes — full,
// empty, esc-interrupting, each InputState, each KeyMode — styled and not.
// Nothing may panic, nothing may exceed its given width, and the result is
// always exactly one row.
func TestRender_WidthStability(t *testing.T) {
	ctxs := []FocusContext{
		{},
		fullContext(),
		{EscInterrupts: true, Verbs: sampleVerbs()},
		{Input: InputFailed, Hint: "send failed", Attention: 1},
		{Input: InputQueued, Hint: "alt+↑ edit queued"},
		{KeyMode: KeyModeRooms, KeyModeCount: 9},
		{Verbs: sampleVerbs()[:1]},
	}

	for ci, ctx := range ctxs {
		for _, sty := range []*tokens.Styler{nil, styler()} {
			m := New(Options{Styler: sty})
			for width := -1; width <= 110; width++ {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("ctx %d width %d panicked: %v", ci, width, r)
						}
					}()
					out := m.Render(ctx, width)
					if strings.Contains(out, "\n") {
						t.Fatalf("ctx %d width %d produced more than one row: %q", ci, width, out)
					}
					if w := ansi.StringWidth(out); width > 0 && w > width {
						t.Fatalf("ctx %d width %d rendered %d cells: %q", ci, width, w, out)
					}
					if width <= 0 && out != "" {
						t.Fatalf("ctx %d width %d rendered non-empty: %q", ci, width, out)
					}
				}()
			}
		}
	}
}

// TestEmptyContextRendersNothingButTheHelpDoor: a fully zero-valued
// FocusContext has nothing to say except the one permanent column.
func TestEmptyContextRendersNothingButTheHelpDoor(t *testing.T) {
	m := New(Options{})
	out := m.Render(FocusContext{}, 80)
	if out != helpDoorText {
		t.Fatalf("empty context = %q, want just the help door %q", out, helpDoorText)
	}
}

// TestAttentionOutranksEverything: at a width that cannot hold the whole
// line, the attention badge is the last thing dropped, before even the
// permanent help door.
func TestAttentionOutranksEverything(t *testing.T) {
	m := New(Options{})
	ctx := fullContext()
	// A width that fits only one or two columns.
	out := m.Render(ctx, 4)
	if !strings.Contains(out, tokens.GlyphNeedsHuman+"2") {
		t.Fatalf("attention badge missing at width 4: %q", out)
	}
}

// TestPriorityDroppingOrder walks the width down from "everything fits" and
// checks that columns disappear in exactly the stated priority order —
// lowest priority (scope) first, attention last — matching the drop order
// tokens.FooterColumnOrder documents for the columns this package shares
// with it.
func TestPriorityDroppingOrder(t *testing.T) {
	m := New(Options{})
	ctx := fullContext()

	full := m.Render(ctx, 500)
	for _, want := range []string{"attention", "help", "hint", "keymode", "verbs", "health", "toast", "scope"} {
		if !containsColumn(full, want, ctx) {
			t.Fatalf("full-width render missing column %q: %q", want, full)
		}
	}

	// Drop order, lowest priority first: scope, toast, health, verbs,
	// keymode, hint, help, attention.
	dropOrder := []string{"scope", "toast", "health", "verbs", "keymode", "hint", "help"}

	present := map[string]bool{
		"attention": true, "help": true, "hint": true, "keymode": true,
		"verbs": true, "health": true, "toast": true, "scope": true,
	}
	prevWidth := 500
	for _, col := range dropOrder {
		// Binary-search-free: shrink width one cell at a time from the last
		// checkpoint until this column's content is no longer present, and
		// confirm nothing HIGHER priority dropped first.
		w := prevWidth
		for w > 0 {
			out := m.Render(ctx, w)
			if !containsColumn(out, col, ctx) {
				break
			}
			w--
		}
		if w <= 0 {
			t.Fatalf("column %q never dropped down to width 0", col)
		}
		present[col] = false
		out := m.Render(ctx, w)
		for id, stillPresent := range present {
			if !stillPresent {
				continue
			}
			if !containsColumn(out, id, ctx) {
				t.Fatalf("at width %d, %q dropped before lower-priority %q was gone: %q", w, id, col, out)
			}
		}
		prevWidth = w
	}
}

// containsColumn is a coarse but sufficient membership test for the
// synthetic content TestPriorityDroppingOrder builds.
func containsColumn(out, id string, ctx FocusContext) bool {
	switch id {
	case "attention":
		return strings.Contains(out, tokens.GlyphNeedsHuman+"2")
	case "help":
		return strings.Contains(out, helpDoorText)
	case "hint":
		return strings.Contains(out, ctx.Hint)
	case "keymode":
		return strings.Contains(out, "answer") || strings.Contains(out, "rooms")
	case "verbs":
		return strings.Contains(out, "cancel")
	case "health":
		return strings.Contains(out, "mcp")
	case "toast":
		return strings.Contains(out, "cancelled elsewhere")
	case "scope":
		return strings.Contains(out, "wisp-parity")
	}
	return false
}

// TestVerbsCapAtThree: however many entries the wiring hands in, at most
// three render (5.22 rule 4).
func TestVerbsCapAtThree(t *testing.T) {
	m := New(Options{})
	entries := []registry.Entry{
		{Verb: "one", Key: "1"}, {Verb: "two", Key: "2"}, {Verb: "three", Key: "3"},
		{Verb: "four", Key: "4"}, {Verb: "five", Key: "5"},
	}
	out := m.Render(FocusContext{Verbs: entries}, 500)
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing expected verb %q: %q", want, out)
		}
	}
	for _, notWant := range []string{"four", "five"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("verb %q should have been capped away: %q", notWant, out)
		}
	}
}

// TestVerbLabelFallsBackThroughKeySlashVerb pins the three shapes a
// registry.Entry's accelerator can take.
func TestVerbLabelFallsBackThroughKeySlashVerb(t *testing.T) {
	tests := []struct {
		name string
		e    registry.Entry
		want string
	}{
		{"key", registry.Entry{Verb: "cancel", Key: "c", Slash: "cancel"}, "c cancel"},
		{"slash only", registry.Entry{Verb: "focus tasks", Slash: "tasks"}, "/tasks focus tasks"},
		{"neither", registry.Entry{Verb: "revise"}, "revise"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verbLabel(tt.e); got != tt.want {
				t.Errorf("verbLabel(%+v) = %q, want %q", tt.e, got, tt.want)
			}
		})
	}
}

// TestEscInterruptOutranksInputHint: when EscInterrupts is true, the hint
// column shows the interrupt phrase even though Hint carries something
// else — an interrupt in flight is more urgent than the composer's own
// state.
func TestEscInterruptOutranksInputHint(t *testing.T) {
	m := New(Options{})
	out := m.Render(FocusContext{EscInterrupts: true, Input: InputTyped, Hint: "↵ send"}, 80)
	if !strings.Contains(out, escInterruptHint) {
		t.Fatalf("esc-interrupt hint missing: %q", out)
	}
	if strings.Contains(out, "send") {
		t.Fatalf("input hint should have been overridden by esc-interrupt: %q", out)
	}
}

// TestHintOnlyWhenTrueOrSet: the hint column never appears out of nowhere —
// both EscInterrupts=false and Hint="" together mean no hint at all.
func TestHintOnlyWhenTrueOrSet(t *testing.T) {
	m := New(Options{})
	out := m.Render(FocusContext{Input: InputEmpty}, 80)
	if strings.Contains(out, escInterruptHint) {
		t.Fatalf("interrupt hint present with EscInterrupts=false: %q", out)
	}
}

// TestFailedInputHintIsColoredCoral pins the one colour decision this
// package makes on the wiring's behalf: a failed send is coral, matching
// every other failure this surface draws (statusPane's own err handling).
func TestFailedInputHintIsColoredCoral(t *testing.T) {
	m := New(Options{Styler: styler()})
	out := m.Render(FocusContext{Input: InputFailed, Hint: "send failed"}, 80)
	want := styler().PaintToken("send failed", tokens.Coral)
	if !strings.Contains(out, want) {
		t.Fatalf("failed-input hint not painted coral:\ngot  %q\nwant substring %q", out, want)
	}
}

// TestKeyModeText pins the digit-precedence wording from 5.22's checklist.
func TestKeyModeText(t *testing.T) {
	tests := []struct {
		mode KeyMode
		n    int
		want string
	}{
		{KeyModeAnswer, 3, "1" + enDash + "3 answer"},
		{KeyModeRooms, 9, "1" + enDash + "9 rooms"},
	}
	for _, tt := range tests {
		if got := keyModeText(tt.mode, tt.n); got != tt.want {
			t.Errorf("keyModeText(%v, %d) = %q, want %q", tt.mode, tt.n, got, tt.want)
		}
	}
}

// TestKeyModeOmittedWithoutCount: KeyModeNone, or a non-positive count,
// means no indicator — a range of nothing is not information.
func TestKeyModeOmittedWithoutCount(t *testing.T) {
	m := New(Options{})
	for _, ctx := range []FocusContext{
		{KeyMode: KeyModeNone, KeyModeCount: 3},
		{KeyMode: KeyModeAnswer, KeyModeCount: 0},
		{KeyMode: KeyModeAnswer, KeyModeCount: -1},
	} {
		out := m.Render(ctx, 80)
		if strings.Contains(out, "answer") || strings.Contains(out, "rooms") {
			t.Fatalf("keymode indicator present for %+v: %q", ctx, out)
		}
	}
}

// TestHealthOmittedWhenEmpty and TestToastOmittedWhenEmpty pin 10.5.23's
// "show only when pending" rule structurally: an empty field never draws an
// empty column.
func TestHealthAndToastOmittedWhenEmpty(t *testing.T) {
	m := New(Options{})
	out := m.Render(FocusContext{}, 80)
	if strings.Count(out, sep) != 0 {
		t.Fatalf("empty context should draw a single bare column, no separators: %q", out)
	}
}

// TestHelpDoorAlwaysPresentAtGenerousWidth: the permanent door survives
// whenever there is any reasonable room at all.
func TestHelpDoorAlwaysPresentAtGenerousWidth(t *testing.T) {
	m := New(Options{})
	out := m.Render(fullContext(), 200)
	if !strings.Contains(out, helpDoorText) {
		t.Fatalf("help door missing at generous width: %q", out)
	}
}

// TestNeverPanicsWithNilStyler mirrors the composer package's own posture
// for a missing Styler.
func TestNeverPanicsWithNilStyler(t *testing.T) {
	m := New(Options{})
	out := m.Render(fullContext(), 80)
	if out == "" {
		t.Fatal("nil-styler render produced nothing")
	}
}
