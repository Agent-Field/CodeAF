package tui3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A chat on another machine keeps `running on <device>` at every width and
// title length: the title is cut with an ellipsis before the note goes.
func TestMachineRowKeepsNoteBesideLongTitle(t *testing.T) {
	pal := newTestPalette()
	for _, width := range []int{60, 80, 100, 120, 160} {
		for _, n := range []int{10, 35, 70} {
			title := strings.Repeat("a", n)
			t.Run(fmt.Sprintf("w%d_t%d", width, n), func(t *testing.T) {
				cell := &homeCell{kind: cellRow, panel: panelSessions, title: title,
					note: "running on studio", keepNote: true, right: "now"}
				row := plain(homeCellBody(cell, width, pal, false))
				if !strings.Contains(row, "running on studio") || !strings.HasSuffix(row, "now") {
					t.Fatalf("note or age lost: %q", row)
				}
				if got := ansi.StringWidth(row); got > width {
					t.Fatalf("row is %d cells in %d: %q", got, width, row)
				}
				if whole := n <= width-len("running on studio now")-4; whole != strings.HasPrefix(row, title) {
					t.Fatalf("title whole=%v, row %q", whole, row)
				}
			})
		}
	}
}
