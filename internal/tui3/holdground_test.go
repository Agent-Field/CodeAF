package tui3

// A GROUND STEP OWNS EVERY CELL IT COVERS. These hold styles.go's [holdGround]
// to that, at both doors it matters at: the palette step itself, and a real
// sweep over a real transcript row.
//
// The bug they pin (#294) was that a ground step was one background code, the
// text, and one reset — true only of text carrying no background of its own.
// An inline code span carries one, and it closes with "\x1b[49;39m": nested
// inside a selection, that switched the highlight off for the whole rest of the
// row. A sweep over a line that opened with a code span lit four cells of
// sixty-four, which reads as a selection that never happened.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// litCells counts the printable cells drawn while the OUTER ground — the first
// background the string sets — is still standing, which is what a person sees
// wearing the selection. It reads SGR the way a terminal does: parameters come
// in compound lists ("49;39"), and an inner background REPLACES the outer one
// rather than nesting inside it.
func litCells(s string) int {
	outer, cur, n := "", "", 0
	rest := s
	for len(rest) > 0 {
		i := strings.Index(rest, "\x1b[")
		if i < 0 {
			if cur != "" && cur == outer {
				n += ansi.StringWidth(rest)
			}
			break
		}
		if cur != "" && cur == outer {
			n += ansi.StringWidth(rest[:i])
		}
		j := strings.IndexByte(rest[i:], 'm')
		if j < 0 {
			break
		}
		params := strings.Split(rest[i+2:i+j], ";")
		for k := 0; k < len(params); k++ {
			switch params[k] {
			case "48":
				if k+1 < len(params) && params[k+1] == "5" {
					cur, k = strings.Join(params[k:k+3], ";"), k+2
				} else if k+1 < len(params) && params[k+1] == "2" {
					cur, k = strings.Join(params[k:k+5], ";"), k+4
				}
				if outer == "" {
					outer = cur
				}
			case "49", "0", "":
				cur = ""
			}
		}
		rest = rest[i+j+1:]
	}
	return n
}

// Every ground step covers every cell it is given, whatever paint the text it
// is handed already carries.
func TestAGroundStepCoversEveryCellItIsGiven(t *testing.T) {
	for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256} {
		p := palette{profile: profile}
		for _, tc := range []struct{ door, text string }{
			{"a row with one inline code span", "held the " + p.chip("which.go") + ", and more"},
			{"a row that OPENS with a code span", p.chip("which.go") + " was deleted"},
			{"a row with two code spans", p.chip("wildcard.go") + ", " + p.chip("revendor.sh") + ", etc."},
			{"a row that is only a code span", p.chip("go.mod")},
			{"a plain row", "the diff: 1,917 insertions"},
		} {
			w := ansi.StringWidth(tc.text)
			for _, step := range []struct{ name, got string }{
				{"mark", p.mark(tc.text, w)},
				{"selected", p.selected(tc.text, w)},
			} {
				if lit := litCells(step.got); lit != w {
					t.Errorf("%v / %s / %s: %d of %d cells wear the ground",
						profile, tc.door, step.name, lit, w)
				}
			}
		}
	}
}

// A ground step pads to its width, and the padding wears the ground too — that
// is how a selection running past a short row shows how far it goes.
func TestAGroundStepPaintsItsPaddingToo(t *testing.T) {
	p := palette{profile: tokens.TrueColor}
	text := "held " + p.chip("x")
	if lit := litCells(p.mark(text, 40)); lit != 40 {
		t.Errorf("a step padded to 40 wears the ground on %d cells", lit)
	}
}

// The INK is left alone: only the plane under the text is made one, so a marked
// code span keeps its own colour.
func TestAGroundStepLeavesTheInkAlone(t *testing.T) {
	p := palette{profile: tokens.TrueColor}
	text := "held " + p.chip("which.go")
	got := p.mark(text, ansi.StringWidth(text))
	if !strings.Contains(got, "38;2;") {
		t.Errorf("the marked span lost its ink: %q", got)
	}
}

// Text with no escape in it comes back untouched.
func TestHoldGroundLeavesPlainTextAlone(t *testing.T) {
	if got := holdGround("just words", "48;5;7"); got != "just words" {
		t.Errorf("holdGround rewrote plain text: %q", got)
	}
}

// Every background form the styler can emit is answered.
func TestRegroundAnswersEveryBackgroundForm(t *testing.T) {
	const g = "48;5;7"
	for _, tc := range []struct{ in, want string }{
		{"48;5;236", g},             // the 256 ground
		{"48;2;27;27;37", g},        // the true-colour ground
		{"49", g},                   // a bare background clear
		{"49;39", g + ";39"},        // the compound clear prose actually emits
		{"41", g},                   // a sixteen-colour ground
		{"104", g},                  // a bright sixteen-colour ground
		{"0", "0;" + g},             // a full reset re-lays the ground behind it
		{"", "0;" + g},              // which the terminal also spells as nothing
		{"1;38;2;1;2;3", ""},        // no background: untouched, and reported so
	} {
		got, hit := reground(tc.in, g)
		if tc.want == "" {
			if hit || got != tc.in {
				t.Errorf("reground(%q) = %q, %v; it carries no background", tc.in, got, hit)
			}
			continue
		}
		if !hit || got != tc.want {
			t.Errorf("reground(%q) = %q, %v; want %q, true", tc.in, got, hit, tc.want)
		}
	}
}

// END TO END, at the door: a real sweep over a real assistant message carrying
// an inline code span lights every cell of the text it is about to copy.
func TestASweepOverACodeSpanLightsEveryCellItCopies(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.width, a.height = 80, 30
	a.entries = append(a.entries,
		entry{kind: entryUser, text: "what was deleted?"},
		entry{kind: entryAssistant, settled: true,
			text: "The `internal/swepro/` directory was deleted, along with the rest."},
	)
	a.touch()

	y := screenRowWith(t, a, "directory was deleted")
	drive(t, a, tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: 79, Y: y, Button: tea.MouseLeft})

	frame, _, _ := a.frame()
	var got string
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(ansi.Strip(line), "directory was deleted") {
			got = line
			break
		}
	}
	if got == "" {
		t.Fatal("the swept row is not in the frame")
	}
	// The whole row is swept, so the whole row is lit — padding included.
	if lit, want := litCells(got), ansi.StringWidth(ansi.Strip(got)); lit != want {
		t.Errorf("the sweep lit %d of the row's %d cells: the highlight stops at "+
			"the inline code span", lit, want)
	}
}

// AND WHAT LANDS IS WHAT WAS LIT. The yank reads plain text, so the clipboard
// was always right; this holds the two together so a future change cannot make
// the highlight honest by making the copy wrong.
func TestTheSweepCopiesExactlyWhatItLit(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.width, a.height = 80, 30
	a.entries = append(a.entries,
		entry{kind: entryUser, text: "what was deleted?"},
		entry{kind: entryAssistant, settled: true,
			text: "The `internal/swepro/` directory was deleted, along with the rest."},
	)
	a.touch()
	y := screenRowWith(t, a, "directory was deleted")
	drive(t, a, tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: 79, Y: y, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: 79, Y: y, Button: tea.MouseLeft})
	if copied := rawPayload(t, runCmd(cmd)); !strings.Contains(
		copied, "The internal/swepro/ directory was deleted, along with the rest.") {
		t.Errorf("the sweep copied %q", copied)
	}
}
