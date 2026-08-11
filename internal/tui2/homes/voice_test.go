package homes

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func mics() []Mic {
	out := []Mic{}
	for _, state := range []MicState{
		MicUnavailable, MicIdle, MicStarting, MicListening, MicTranscribing, MicRefused,
	} {
		for _, target := range []rail.ComposerMode{
			rail.ComposerNone, rail.ComposerChat, rail.ComposerSteer, rail.ComposerDisabled,
		} {
			out = append(out,
				Mic{State: state, Target: target},
				Mic{State: state, Target: target, Elapsed: 96 * time.Second},
				Mic{State: state, Target: target, Reason: "visitor window — only the resident may act"},
				Mic{State: state, Target: target, Reason: "\x1b[2Jcleared"},
			)
		}
	}
	return out
}

func TestMicNeverOverflowsAndNeverPanics(t *testing.T) {
	for name, st := range stylers() {
		for _, m := range mics() {
			for width := 0; width <= 60; width++ {
				out := m.Render(st, width)
				if w := blocks.Width(out); w > width {
					t.Fatalf("%s %v w=%d: %d cells: %q", name, m.State, width, w, out)
				}
				if strings.ContainsAny(out, "\n\r") {
					t.Fatalf("%s %v w=%d: newline in %q", name, m.State, width, out)
				}
			}
		}
	}
}

// A nil recorder or transcriber means no control at all — the seam internal/voice
// exposes can be absent, and a permanently dead mic on every composer in the
// product would be an affordance that lies, four hundred frames a day.
func TestANilVoiceSeamMeansNoControlAtAll(t *testing.T) {
	m := Mic{}
	if m.State != MicUnavailable {
		t.Fatal("the zero mic is not the unavailable one")
	}
	if !m.Empty() || m.Text() != "" || m.Width(nil) != 0 {
		t.Fatalf("an unavailable mic drew %q", m.Text())
	}
	for width := 0; width <= 20; width++ {
		if out := m.Render(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal), width); out != "" {
			t.Fatalf("w=%d drew %q", width, out)
		}
	}
}

// 5.24's one-way rule. Dictation into a steer line renders visibly one-way, and
// the mark is the composer's OWN steer prompt so the two can never disagree
// about what ↦ means.
func TestDictationIntoASteerLineIsVisiblyOneWay(t *testing.T) {
	chat := Mic{State: MicListening, Target: rail.ComposerChat}
	steer := Mic{State: MicListening, Target: rail.ComposerSteer}
	if strings.Contains(chat.Text(), tokens.GlyphPromptSteer) {
		t.Fatalf("a chat target carried the one-way mark: %q", chat.Text())
	}
	if !strings.Contains(steer.Text(), tokens.GlyphPromptSteer) {
		t.Fatalf("a steer target lost the one-way mark: %q", steer.Text())
	}
	// The mark rides every state the mic can be in, not only while it listens:
	// a person about to press the key deserves to know where the words go.
	for _, state := range []MicState{MicIdle, MicStarting, MicListening, MicTranscribing, MicRefused} {
		m := Mic{State: state, Target: rail.ComposerSteer}
		if !strings.Contains(m.Text(), tokens.GlyphPromptSteer) {
			t.Fatalf("%v lost the one-way mark", state)
		}
	}
}

// The mic mints no glyph. Everything it draws is a slot the 5.17 vocabulary
// already owns, so 12.7's tier upgrades it for free and the width gate already
// covers it.
func TestTheMicBorrowsTheVocabularyAndInventsNothing(t *testing.T) {
	known := map[string]bool{}
	for _, g := range tokens.Vocabulary() {
		known[g.Plain] = true
		if g.NerdFont != "" {
			known[g.NerdFont] = true
		}
	}
	for _, set := range []tokens.GlyphSet{tokens.Plain, tokens.NerdFont} {
		for _, state := range []MicState{MicIdle, MicStarting, MicListening, MicTranscribing, MicRefused} {
			m := Mic{State: state}
			if g := m.glyph(set); !known[g] {
				t.Fatalf("%v/%v drew %q, which is not in the vocabulary", set, state, g)
			}
		}
	}
}

// A listening mic is alive and never amber: it is not asking for anything, and
// 5.16 spends amber on the one meaning.
func TestAListeningMicIsAliveAndNeverAmber(t *testing.T) {
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	amber := tokens.Amber.Fg(tokens.TrueColor, tokens.FocusNormal)
	for _, state := range []MicState{MicIdle, MicStarting, MicListening, MicTranscribing, MicRefused} {
		m := Mic{State: state, Elapsed: time.Minute, Reason: "no"}
		if strings.Contains(m.Render(st, 40), amber) {
			t.Fatalf("%v painted amber", state)
		}
	}
	live := Mic{State: MicListening}
	if live.token() != tokens.ResolveToken(tokens.HueAlive, tokens.StateLive) {
		t.Fatal("a listening mic is not on the live axis")
	}
	if (Mic{State: MicIdle}).token() != tokens.TextTertiary {
		t.Fatal("an idle mic is not chrome")
	}
}

// A refused mic states its limit rather than disappearing (5.20 rule 3), and
// the wiring's words go through the same sanitiser everything else does.
func TestARefusedMicStatesItsLimitSafely(t *testing.T) {
	m := Mic{State: MicRefused, Reason: "\x1b[2Jvisitor window"}
	out := m.Render(tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal), 60)
	if !strings.Contains(out, "visitor window") {
		t.Fatalf("the reason did not reach the row: %q", out)
	}
	if strings.Contains(out, "\x1b[2J") {
		t.Fatalf("an escape survived: %q", out)
	}
}
