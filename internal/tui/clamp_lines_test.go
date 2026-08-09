package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The panes used to pad themselves by rendering the whole clamped block
// through a styled Width. clampLines pads now and the style is gone, so this
// holds the two against each other line by line: ANSI runs, wide runes, an
// exactly-full line, an OSC 8 link, tabs, and an over-wide line that has to
// truncate. Tabs are expanded before the old pipeline measures too, because
// that is the one thing the styled Width did that the clamp could not see —
// and a line clamped before expansion wraps out of the pane anyway.
func TestClampedLinesPadExactlyLikeTheStyleDid(t *testing.T) {
	lines := []string{
		"",
		" ",
		"plain",
		"exactly twenty chars",
		"this line is definitely longer than twenty columns",
		mutedStyle.Render("styled words here"),
		mutedStyle.Render("styled and much much too long for the pane"),
		"日本語のテキストです",
		"日本語のテキストですがとても長い行です",
		"trailing spaces   ",
		"tab\there",
		"\ttabs\tall\tover\tthe\tplace\there",
		bandStyle.Width(20).Render("banded"),
		"a" + strings.Repeat(" ", 19),
		"mixed 日本 ansi " + roseStyle.Render("x"),
		"emoji 🙂 in the middle",
		"🙂🙂🙂🙂🙂🙂🙂🙂🙂🙂🙂",
		"\x1b]8;;file:///tmp/x\x1b\\linked\x1b]8;;\x1b\\ tail",
		strings.Repeat("x", 19) + "…",
		mutedStyle.Faint(true).Render(strings.Repeat("─", 20)),
	}
	for _, width := range []int{1, 8, 20, 60} {
		for _, line := range lines {
			// The pipeline as it was: expand, clamp, render through a Width.
			expanded := strings.ReplaceAll(line, "\t", styleTabStop)
			old := []string{expanded}
			if lipgloss.Width(expanded) > width {
				old[0] = truncate(expanded, width)
			}
			want := lipgloss.NewStyle().Width(width).Render(old[0])

			got := []string{line}
			clampLines(got, width)
			if got[0] != want {
				t.Fatalf("width %d, %q:\n got %q\nwant %q", width, line, got[0], want)
			}
			if measured := lipgloss.Width(got[0]); measured != max(width, 0) && got[0] != "" {
				t.Fatalf("width %d, %q clamped to %d cells", width, line, measured)
			}
		}
	}
}

func TestSpacesIsARunOfBlanks(t *testing.T) {
	for _, count := range []int{-3, 0, 1, 7, len(spaceRun), len(spaceRun) + 5} {
		got := spaces(count)
		if got != strings.Repeat(" ", max(0, count)) {
			t.Fatalf("spaces(%d) = %q", count, got)
		}
	}
}
