package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// A GROUND STEP OWNS EVERY CELL IT COVERS (#294). The sweep's highlight, copy
// mode's selection and the pointer's row all paint through one function,
// palette.background, which used to be one background code, the text, one
// reset — true only of text carrying no background of its own. An inline code
// span sets its own ground and closes with the compound `49;39`, so a sweep
// across a row with one lit the cells before the span and nothing after it,
// while the clipboard still took the whole row: the paint lied about the copy.
// Now every background parameter inside the span is rewritten to the step's own
// ground, and a full reset keeps its reset and re-lays the ground behind it.
// Inks and attributes are untouched, and the selection wins over the chip.

// litCells walks one rendered row and counts the cells whose standing
// background is a 48 ground — the honest count of what reads as lit. The walk
// must track the standing background because the sequence at fault is the
// compound `49;39` and a naive scan for `\x1b[49m` never sees it; and it must
// consume the sub-parameters of 38/48/58 because a 256-colour index of 49 is
// not a background clear.
func litCells(line string) int {
	i, cells, bg := 0, 0, false
	for i < len(line) {
		if line[i] == '\x1b' {
			j := strings.IndexByte(line[i:], 'm')
			if j < 0 {
				break
			}
			seq := line[i : i+j+1]
			i += j + 1
			params := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
			if params == "" || params == "0" {
				bg = false
				continue
			}
			parts := strings.Split(params, ";")
			for k := 0; k < len(parts); k++ {
				switch p := parts[k]; {
				case p == "38" || p == "58" || p == "48":
					if n := colourSubs(parts, k+1); n > 0 {
						k += n
					}
					if p == "48" {
						bg = true
					}
				case p == "49" || p == "0":
					bg = false
				case isBg16(p):
					// the sixteen-colour backgrounds, 40–47 and 100–107
					bg = true
				}
			}
			continue
		}
		r, size := decodeRune(line[i:])
		i += size
		if bg {
			cells += ansi.StringWidth(string(r))
		}
	}
	return cells
}

func decodeRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

// codeSpanApp is the fixture the field failure was seen on: an answer whose
// row carries one inline code span, the way prose draws them on the raised
// plane.
func codeSpanApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.width, a.height = 80, 30
	a.entries = append(a.entries,
		entry{kind: entryUser, text: "what was deleted?"},
		entry{kind: entryAssistant, settled: true, text: "The `internal/swepro/` directory was deleted, along with the rest."},
	)
	a.touch()
	return a
}

// A sweep across a row with an inline code span lights every cell of the row,
// padding included — the bytes the terminal is handed, read back off
// a.frame(). Before the fix this lit 4 of 80: the cells before the span.
func TestASweepOverACodeSpanLightsEveryCellItCopies(t *testing.T) {
	a := codeSpanApp(t)
	y := screenRowWith(t, a, "directory was deleted")

	drive(t, a, tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: 79, Y: y, Button: tea.MouseLeft})

	frame, _, _ := a.frame()
	row := strings.Split(frame, "\n")[y]
	if lit := litCells(row); lit != ansi.StringWidth(ansi.Strip(row)) {
		t.Fatalf("the sweep lit %d of the row's %d cells: the highlight stops at the inline code span",
			lit, ansi.StringWidth(ansi.Strip(row)))
	}
}

// The control on the same sweep: the copy is exactly what was lit, so a future
// change cannot make the highlight honest by making the copy wrong.
func TestTheSweepCopiesExactlyWhatItLit(t *testing.T) {
	a := codeSpanApp(t)
	y := screenRowWith(t, a, "directory was deleted")
	if got := sweep(t, a, 0, y, 79, y); got != "The internal/swepro/ directory was deleted, along with the rest." {
		t.Fatalf("copied %q", got)
	}
}

// Both ground steps on the ground ladder — the sweep's mark and the pointer's
// selected — cover every cell they are given, at TrueColor and ANSI256, over
// the five row shapes a code span can leave a row in.
func TestAGroundStepCoversEveryCellItIsGiven(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  func(p palette) string
	}{
		{"a row with one inline code span", func(p palette) string {
			return p.ink("The ") + p.chip("internal/swepro/") + p.ink(" directory was deleted.")
		}},
		{"a row that OPENS with a code span", func(p palette) string {
			return p.chip("internal/swepro/") + p.ink(" directory was deleted.")
		}},
		{"a row with two code spans", func(p palette) string {
			return p.ink("The ") + p.chip("internal/swepro/") + p.ink(" and ") + p.chip("internal/tui3/") + p.ink(" went.")
		}},
		{"a row that is only a span", func(p palette) string {
			return p.chip("internal/swepro/")
		}},
		{"a plain control", func(p palette) string {
			return p.ink("The directory was deleted.")
		}},
	} {
		for _, profile := range []tokens.Profile{tokens.TrueColor, tokens.ANSI256} {
			p := newPalette(profile, false)
			row := tc.row(p)
			width := ansi.StringWidth(ansi.Strip(row))
			for _, step := range []struct {
				name string
				paint func(string, int) string
			}{
				{"mark", p.mark},
				{"selected", p.selected},
			} {
				t.Run(tc.name+" / "+step.name, func(t *testing.T) {
					painted := step.paint(row, width)
					if lit := litCells(painted); lit != width {
						t.Fatalf("%s covers %d of the %d cells it was given", step.name, lit, width)
					}
				})
			}
		}
	}
}

// Every background form the styler can emit is answered: 48;5;n, 48;2;r;g;b,
// the sixteen-colour 40–47 and 100–107, the bare 49, the compound 49;39 that
// prose actually emits, and 0 — which a terminal also spells as no parameters
// at all.
func TestRegroundAnswersEveryBackgroundForm(t *testing.T) {
	const ground = "48;2;67;76;94"
	for _, tc := range []struct {
		in, want string
	}{
		{"a\x1b[48;5;240mb", "a\x1b[" + ground + "mb"},
		{"a\x1b[48;2;27;27;37mb", "a\x1b[" + ground + "mb"},
		{"a\x1b[49mb", "a\x1b[" + ground + "mb"},
		{"a\x1b[49;39mb", "a\x1b[" + ground + ";39mb"},
		{"a\x1b[0mb", "a\x1b[0m\x1b[" + ground + "mb"},
		{"a\x1b[mb", "a\x1b[m\x1b[" + ground + "mb"},
		{"a\x1b[38;5;244mb\x1b[48;5;17mc", "a\x1b[38;5;244mb\x1b[" + ground + "mc"},
		{"a\x1b[38;2;1;2;3mb\x1b[48;2;4;5;6mc", "a\x1b[38;2;1;2;3mb\x1b[" + ground + "mc"},
	} {
		if got := holdGround(tc.in, ground); got != tc.want {
			t.Errorf("holdGround(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for base := 40; base <= 47; base++ {
		in := "a\x1b[" + itoa(base) + "mb"
		want := "a\x1b[" + ground + "mb"
		if got := holdGround(in, ground); got != want {
			t.Errorf("holdGround(%q) = %q, want %q", in, got, want)
		}
	}
	for base := 100; base <= 107; base++ {
		in := "a\x1b[" + itoa(base) + "mb"
		want := "a\x1b[" + ground + "mb"
		if got := holdGround(in, ground); got != want {
			t.Errorf("holdGround(%q) = %q, want %q", in, got, want)
		}
	}
}

// Plain text is passed through untouched — no escape, no rewrite, no growth.
func TestHoldGroundLeavesPlainTextAlone(t *testing.T) {
	for _, s := range []string{"", "The directory was deleted.", "  ", "internal/swepro/ 目录"} {
		if got := holdGround(s, "48;2;67;76;94"); got != s {
			t.Errorf("holdGround(%q) = %q, want it back unchanged", s, got)
		}
	}
}

// The step holds the ground, not the ink: foreground colours, underline
// colours and attributes keep their codes and their resets, so a marked code
// span keeps its colour, its bold and its italic.
func TestAGroundStepLeavesTheInkAlone(t *testing.T) {
	p := newPalette(tokens.TrueColor, false)
	row := p.ink("The ") + p.chip("internal/swepro/") + p.ink(" went.")
	painted := p.mark(row, ansi.StringWidth(ansi.Strip(row)))
	for _, seq := range []string{
		"\x1b[38;2;198;205;218m", // the ink's open
		"\x1b[39m",               // the ink's close
	} {
		if !strings.Contains(painted, seq) {
			t.Errorf("the marked row lost the ink sequence %q: %q", seq, painted)
		}
	}
	bold := newPalette(tokens.TrueColor, false)
	boldRow := "\x1b[1mThe\x1b[22m " + bold.chip("internal/swepro/")
	boldPainted := bold.mark(boldRow, ansi.StringWidth(ansi.Strip(boldRow)))
	for _, seq := range []string{"\x1b[1m", "\x1b[22m"} {
		if !strings.Contains(boldPainted, seq) {
			t.Errorf("the marked row lost the attribute sequence %q: %q", seq, boldPainted)
		}
	}
}
