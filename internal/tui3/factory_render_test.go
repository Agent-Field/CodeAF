package tui3

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ── EVERY PANE'S LINES ARE EXACTLY ITS WIDTH (owner's floor, 2026-10-09) ────
//
// The owner saw the peek's lines painted over the rows: the risk words, the
// stage row and the body at column 0 across `#1100`, and `thinking  —` in the
// middle of a word of the body. The cause was below the frame (ttyguard.go),
// and these are the laws that say so: on the owner's floor at every width the
// peek opens at, every frame line is one screen row no wider than the
// terminal, carries no control byte and no escape but colour and links, every
// body row is exactly the width with the divider in the rows' column, and
// nothing the peek alone says is ever left of the divider.

// factoryEscapes is every escape sequence in a frame: CSI, OSC, or a bare
// two-byte escape.
var factoryEscapes = regexp.MustCompile("\x1b(\\[[0-9;:?<>=]*[ -/]*[@-~]|\\][^\x07\x1b]*(\x07|\x1b\\\\)|.)")

// factoryFrameLaws checks one frame of the floor or the item page and says
// what broke, "" for nothing.
func factoryFrameLaws(a *app, frame string) string {
	lines := strings.Split(frame, "\n")
	if len(lines) != a.height {
		return "the frame has " + itoa(len(lines)) + " lines for a height of " + itoa(a.height)
	}
	for _, esc := range factoryEscapes.FindAllString(frame, -1) {
		if !strings.HasSuffix(esc, "m") && !strings.HasPrefix(esc, "\x1b]8;") {
			return "the frame carries an escape that is not colour or a link: " + strings.ReplaceAll(esc, "\x1b", "ESC")
		}
	}
	for y, line := range lines {
		if w := ansi.StringWidth(line); w > a.width {
			return "row " + itoa(y) + " is " + itoa(w) + " cells wide on a " + itoa(a.width) + " cell terminal: " + plain(line)
		}
		for _, r := range plain(line) {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
				return "row " + itoa(y) + " carries control rune " + itoa(int(r)) + ": " + plain(line)
			}
		}
	}
	if a.fp.open || a.fp.rowsW >= a.width {
		return ""
	}
	top := placeHeadRows + a.fp.headRows
	for y := top; y < top+a.fp.shown && y < len(lines); y++ {
		row := plain(lines[y])
		if w := ansi.StringWidth(row); w != a.width {
			return "body row " + itoa(y) + " is " + itoa(w) + " cells, not " + itoa(a.width) + ": " + row
		}
		if cell := ansi.Cut(row, a.fp.rowsW, a.fp.rowsW+1); cell != "│" {
			return "body row " + itoa(y) + " has " + cell + " where the divider stands at " + itoa(a.fp.rowsW) + ": " + row
		}
		left := ansi.Cut(row, 0, a.fp.rowsW)
		for _, peekOnly := range []string{"touches engine path resolution", "thinking", "budget"} {
			if strings.Contains(left, peekOnly) {
				return "the peek's `" + peekOnly + "` is left of the divider on row " + itoa(y) + ": " + row
			}
		}
	}
	return ""
}

// THE FLOOR WITH THE PEEK OPEN, AT EVERY WIDTH IT OPENS AT: every item under
// the cursor, then under the pointer, then in the comfortable density, keeps
// every law above.
func TestFactoryFloorLinesAreExactlyTheirWidthAtEveryWidth(t *testing.T) {
	for _, size := range [][2]int{{150, 40}, {200, 50}, {230, 60}, {250, 60}, {263, 44}} {
		a := factoryPerfLab(t, size[0], size[1])
		a.fp.backlog = true
		walk := len(a.factoryWalkNow())
		for variant, name := range []string{"cursor", "pointer", "comfortable"} {
			a.fp.comfy = variant == 2
			for c := 0; c < walk; c += 7 {
				a.fp.cursor, a.fp.hover = c, -1
				if variant == 1 {
					a.fp.hover = c
				}
				f, _, _ := a.frame()
				if broke := factoryFrameLaws(a, f); broke != "" {
					t.Fatalf("%dx%d, %s on walk %d: %s", size[0], size[1], name, c, broke)
				}
				if a.fp.rowsW >= a.width {
					t.Fatalf("%dx%d: the peek did not open", size[0], size[1])
				}
			}
		}
	}
}

// THE ITEM PAGE KEEPS THE SAME LAWS at the same widths: the top bar, the left
// column and the center each end inside the terminal, on every item.
func TestFactoryItemPageLinesFitAtEveryWidth(t *testing.T) {
	for _, size := range [][2]int{{150, 40}, {250, 60}} {
		a := factoryPerfLab(t, size[0], size[1])
		a.fp.backlog = true
		walk := len(a.factoryWalkNow())
		for c := 0; c < walk; c += 11 {
			a.fp.cursor, a.fp.open = c, true
			f, _, _ := a.frame()
			if broke := factoryFrameLaws(a, f); broke != "" {
				t.Fatalf("%dx%d item page on walk %d: %s", size[0], size[1], c, broke)
			}
		}
	}
}
