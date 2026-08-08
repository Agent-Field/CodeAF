package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// dockWidths are the widths a docked pane actually gets: the journey's own
// 60-column answer, the width just under the rail's threshold, and an ordinary
// full window.
var dockWidths = []int{60, 72, 99, 100, 120}

// assertFitsWidth is the invariant every surface owes a narrow frame: no line
// wider than the terminal, because one over-wide line soft-wraps and every row
// below it shifts by one for the rest of the session.
func assertFitsWidth(t *testing.T, frame string, width int, what string) {
	t.Helper()
	for index, line := range strings.Split(frame, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("%s at width %d produced a %d-cell line %d: %q",
				what, width, got, index, ansi.Strip(line))
		}
	}
}
