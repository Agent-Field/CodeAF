package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// Hover uses the real motion router, and must leave both the spelling and the
// click columns unchanged while adding an underline only to the hovered word.
func TestMessageBoxModelAndProjectUnderlineOnHover(t *testing.T) {
	for _, home := range []bool{true, false} {
		name := "conversation"
		if home {
			name = "home"
		}
		t.Run(name, func(t *testing.T) {
			_, a := drafting(t)
			a.width, a.height = 240, 40
			a.pal = newPalette(tokens.TrueColor, false)
			a.workspace, a.target.where = "/tmp/hover-project", "/tmp/hover-project"
			if home {
				a.showPage(pageHome)
			} else {
				a.leavePlace()
			}
			draw := func() string {
				if home {
					lines, _, _, _ := a.homeFrame(a.width, a.height)
					return strings.Join(lines, "\n")
				}
				return frame(a)
			}
			draw()
			// THE MODEL IS ON THE SEAM AND THE PROJECT ON THE KEYS ROW, on both
			// boxes (hometip.go, footswap.go's [app.hintRow]).
			type door struct {
				span    hudSpan
				row     int
				painted string
			}
			pal := a.pal
			var doors []door
			if home {
				pal = pal.onPlaces()
				doors = []door{
					{a.targetModelSpan, a.targetRow, pal.underline(pal.seamModel("m"))},
					{a.targetFolderSpan, a.footRow, pal.underline(pal.dim("/tmp/hover-project"))},
				}
			} else {
				doors = []door{
					{a.seamModelSpan, seamRowY(a), pal.underline(pal.seamModel("m"))},
					{a.seamProjectSpan, markedRowY(a, chromeStatus, 0), pal.underline(pal.dim("/tmp/hover-project"))},
				}
			}
			for _, target := range doors {
				if !target.span.pressable() {
					t.Fatalf("missing span: %+v", target.span)
				}
				drive(t, a, tea.MouseMotionMsg{X: target.span.from, Y: target.row})
				if text := draw(); !strings.Contains(text, target.painted) {
					t.Fatalf("the hovered seam field at %+v did not underline", target.span)
				}
				drive(t, a, tea.MouseMotionMsg{X: 0, Y: 1})
				if text := draw(); strings.Contains(text, target.painted) {
					t.Fatal("the underline stayed after the pointer left")
				}
			}
		})
	}
}
