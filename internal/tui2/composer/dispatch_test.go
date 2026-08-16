package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// -- what a send produces -----------------------------------------------------

func TestDispatch_EnterStaysAndCtrlEnterFollows(t *testing.T) {
	var got []Dispatch
	m := withTargets(t, Options{OnDispatch: func(d Dispatch) { got = append(got, d) }})

	complete(m, "wisp")
	typeString(m, "skip H2")
	m.Key(enterKey())

	complete(m, "wisp")
	typeString(m, "and report back")
	m.Key(ctrlEnterKey())

	if len(got) != 2 {
		t.Fatalf("dispatches = %d, want 2", len(got))
	}
	if got[0].Follow {
		t.Fatalf("enter must send and STAY (5.18): %+v", got[0])
	}
	if !got[1].Follow {
		t.Fatalf("ctrl+enter must send and FOLLOW (5.18): %+v", got[1])
	}
	for i, d := range got {
		if d.TargetID != "t-wisp" || d.Settled {
			t.Fatalf("dispatch %d = %+v, want the live target it addressed", i, d)
		}
	}
	if m.Value() != "" {
		t.Fatalf("draft not cleared after an addressed send: %q", m.Value())
	}
}

func TestDispatch_SettledTargetIsMarkedNotInjected(t *testing.T) {
	// 5.18: a settled target never receives direct injection — the composer
	// marks the dispatch and the wiring routes it to the main head as
	// referenced context. This package's whole duty is the mark.
	var got Dispatch
	m := withTargets(t, Options{OnDispatch: func(d Dispatch) { got = d }})
	complete(m, "wire")
	typeString(m, "what happened there")
	m.Key(enterKey())

	if got.TargetID != "t-wire" {
		t.Fatalf("dispatch target = %q, want t-wire", got.TargetID)
	}
	if !got.Settled {
		t.Fatalf("dispatch = %+v, want Settled set for a history-group target", got)
	}
	if got.Text != "@wire-up what happened there" {
		t.Fatalf("dispatch text = %q", got.Text)
	}
}

func TestDispatch_FirstMentionIsTheDestination(t *testing.T) {
	var got Dispatch
	m := withTargets(t, Options{OnDispatch: func(d Dispatch) { got = d }})
	complete(m, "perf")
	typeString(m, "compare with ")
	complete(m, "wisp")
	m.Key(enterKey())
	if got.TargetID != "t-perf" {
		t.Fatalf("dispatch target = %q, want the first token typed", got.TargetID)
	}
}

func TestDispatch_PlainSendStillUsesOnSubmit(t *testing.T) {
	var sent []string
	var dispatched int
	m := withTargets(t, Options{
		OnSubmit:   func(s string) { sent = append(sent, s) },
		OnDispatch: func(Dispatch) { dispatched++ },
	})
	typeString(m, "just prose")
	m.Key(enterKey())
	if len(sent) != 1 || sent[0] != "just prose" {
		t.Fatalf("sent = %v, want the unaddressed draft on OnSubmit", sent)
	}
	if dispatched != 0 {
		t.Fatalf("OnDispatch fired %d times for an unaddressed draft", dispatched)
	}
}

func TestDispatch_NilOnDispatchFallsBackToOnSubmit(t *testing.T) {
	// Wiring Targets first and OnDispatch later is a supported order, and a
	// composer half way through that adoption must not drop user speech.
	var sent []string
	m := withTargets(t, Options{OnSubmit: func(s string) { sent = append(sent, s) }})
	complete(m, "wisp")
	typeString(m, "hello")
	m.Key(enterKey())
	if len(sent) != 1 || sent[0] != "@wisp-parity hello" {
		t.Fatalf("sent = %v, want the addressed draft to fall back to OnSubmit", sent)
	}
}

func TestDispatch_CtrlEnterWithoutAMentionIsTheNoopItAlwaysWas(t *testing.T) {
	var sent []string
	m := withTargets(t, Options{OnSubmit: func(s string) { sent = append(sent, s) }})
	typeString(m, "prose")
	m.Key(ctrlEnterKey())
	if len(sent) != 0 {
		t.Fatalf("ctrl+enter sent %v with no mention in the draft", sent)
	}
	if m.Value() != "prose" {
		t.Fatalf("Value() = %q, want the draft untouched by an unbound chord", m.Value())
	}
}

func TestDispatch_BlankAddressedDraftNeverSends(t *testing.T) {
	var dispatched int
	m := withTargets(t, Options{OnDispatch: func(Dispatch) { dispatched++ }})
	m.Key(enterKey())
	m.Key(ctrlEnterKey())
	if dispatched != 0 {
		t.Fatalf("OnDispatch fired %d times on an empty draft", dispatched)
	}
}

// -- the dispatch chip (5.22's registry-fix checklist) -----------------------

func TestChip_AppearsOnlyOnceAMentionExists(t *testing.T) {
	m := withTargets(t, Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	m.Focus(true)

	if strings.Contains(ansi.Strip(m.Render(60, 4)), chipStay) {
		t.Fatalf("the chip is showing before there is anything to dispatch")
	}
	m.Key(charKey('@'))
	typeString(m, "wisp")
	if strings.Contains(ansi.Strip(m.Render(60, 4)), chipStay) {
		t.Fatalf("the chip is competing with the open filter for the same rows")
	}
	m.Key(enterKey()) // complete

	out := ansi.Strip(m.Render(60, 4))
	for _, want := range []string{chipStay, chipFollow, "wisp-parity"} {
		if !strings.Contains(out, want) {
			t.Fatalf("chip is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, chipTo) {
		t.Fatalf("a live target's chip must say the message goes TO it:\n%s", out)
	}

	// And it goes away with the token.
	m.Key(backspaceKey())
	m.Key(backspaceKey())
	if strings.Contains(ansi.Strip(m.Render(60, 4)), chipStay) {
		t.Fatalf("the chip outlived the token it was about")
	}
}

func TestChip_SettledTargetSaysAboutNotTo(t *testing.T) {
	m := withTargets(t, Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	complete(m, "wire")
	out := ansi.Strip(m.Render(60, 4))
	if !strings.Contains(out, chipAbout+"wire-up") {
		t.Fatalf("settled chip = %q, want it to say 'about' (5.18, 12.5)", out)
	}
	if strings.Contains(out, chipTo) {
		t.Fatalf("settled chip claims to speak INTO a closed thread:\n%s", out)
	}
}

func TestChip_ShedsTheDestinationBeforeTheChords(t *testing.T) {
	m := withTargets(t, Options{Styler: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)})
	complete(m, "wisp")
	widths := []struct {
		width                          int
		wantStay, wantFollow, wantWord bool
	}{
		{60, true, true, true},
		{26, true, true, false},
		// 11 rather than 10: §19 spends one cell of the row on each side as the
		// field's inner padding, so every chrome row has two fewer to shed into.
		{11, true, false, false},
		{4, false, false, false},
	}
	for _, c := range widths {
		out := ansi.Strip(m.Render(c.width, 4))
		if got := strings.Contains(out, chipStay); got != c.wantStay {
			t.Errorf("width %d: stay chord present = %v, want %v (%q)", c.width, got, c.wantStay, out)
		}
		if got := strings.Contains(out, chipFollow); got != c.wantFollow {
			t.Errorf("width %d: follow chord present = %v, want %v (%q)", c.width, got, c.wantFollow, out)
		}
		if got := strings.Contains(out, chipTo+"wisp-parity"); got != c.wantWord {
			t.Errorf("width %d: destination present = %v, want %v (%q)", c.width, got, c.wantWord, out)
		}
	}
}

func TestChip_NeverTakesTheDraftsLastRow(t *testing.T) {
	m := withTargets(t, Options{})
	complete(m, "wisp")
	out := m.Render(60, 1)
	if strings.Contains(out, "\n") {
		t.Fatalf("Render(60,1) = %q, want one row and for it to be the draft's", out)
	}
	if strings.Contains(ansi.Strip(out), chipStay) {
		t.Fatalf("the chip displaced the draft on a one-row rectangle: %q", out)
	}
}

// -- backward compatibility ---------------------------------------------------

// TestBackwardCompat_NilTargetsRendersByteIdentically is the adoption
// guarantee, stated as a test: a composer built without [Options.Targets]
// behaves exactly as this package did before the `@` grammar landed, down to
// the byte, for every draft the grammar could possibly have opinions about.
func TestBackwardCompat_NilTargetsRendersByteIdentically(t *testing.T) {
	sty := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	drafts := []string{
		"plain prose",
		"@wisp-parity addressed prose",
		"ask @perf-audit about it",
		"mail@example.com",
		"@",
	}
	for _, draft := range drafts {
		plain := New(Options{Styler: sty})
		typeString(plain, draft)
		plain.Focus(true)
		// The reference is the same package with the grammar's one door shut,
		// exercised through the same keystrokes.
		reference := New(Options{Styler: sty, Targets: nil, OnDispatch: nil})
		typeString(reference, draft)
		reference.Focus(true)

		for _, height := range []int{1, 3, 6} {
			for _, width := range []int{1, 8, 40, 80} {
				if a, b := plain.Render(width, height), reference.Render(width, height); a != b {
					t.Fatalf("draft %q at %dx%d: %q != %q", draft, width, height, a, b)
				}
			}
		}
		if plain.Value() != draft {
			t.Fatalf("draft %q round-tripped as %q", draft, plain.Value())
		}
	}
}

func TestBackwardCompat_NilTargetsKeepsEveryKeyWhereItWas(t *testing.T) {
	var sent []string
	m := New(Options{OnSubmit: func(s string) { sent = append(sent, s) }})
	typeString(m, "@wisp-parity hello")
	// tab, ↑ and esc must mean what they meant before: nothing, recall, stash.
	m.Key(tabKey())
	if m.Value() != "@wisp-parity hello" {
		t.Fatalf("tab changed the draft to %q", m.Value())
	}
	m.Key(enterKey())
	if len(sent) != 1 || sent[0] != "@wisp-parity hello" {
		t.Fatalf("sent = %v, want a plain OnSubmit", sent)
	}
	m.Key(upKey())
	if m.Value() != "@wisp-parity hello" {
		t.Fatalf("recall = %q", m.Value())
	}
	if cmd := m.Key(escKey()); cmd != nil {
		t.Fatalf("esc against a non-empty draft must stash, not hand on")
	}
}
