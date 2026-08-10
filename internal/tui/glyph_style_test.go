package tui

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

// The rail's glyph inks moved out of the render and into the file's own table
// of styles. The bytes they paint may not have moved with them, including the
// explicit Bold(false) the old code wrote on every settled node.
func TestRailGlyphsPaintTheSameBytes(t *testing.T) {
	now := time.Date(2026, time.August, 6, 14, 0, 0, 0, time.Local)
	model := New(&fakeBackend{}, "glyphs")
	tint := func(color lipgloss.AdaptiveColor, dimmed bool) lipgloss.AdaptiveColor {
		if dimmed {
			return muted
		}
		return color
	}
	nodes := []store.Node{
		{ID: "folded", FoldRoot: true},
		{ID: "done", Status: store.Done},
		{ID: "flashing", Status: store.Done, FinishedAt: now.Add(-time.Second)},
		{ID: "running", Status: store.Running},
		{ID: "claimed", Status: store.Claimed},
		{ID: "failed", Status: store.Failed},
		{ID: "cancelled", Status: store.Cancelled},
		{ID: "pending", Status: store.Pending},
	}
	for _, node := range nodes {
		for _, dimmed := range []bool{false, true} {
			for _, frame := range []int{0, 3} {
				model.spinnerFrame = frame
				want, wantAnimated := "", false
				switch {
				case node.FoldRoot:
					want = lipgloss.NewStyle().Foreground(tint(powder, dimmed)).Render("◆")
				case node.Status == store.Done:
					want = lipgloss.NewStyle().Foreground(tint(mint, dimmed)).
						Bold(model.completionFlashing(node, now)).Render("●")
				case node.Status == store.Claimed || node.Status == store.Running:
					want = peachStyle.Render("● " + spinnerFrames[frame%len(spinnerFrames)])
					wantAnimated = true
				case node.Status == store.Failed || node.Status == store.Cancelled:
					want = lipgloss.NewStyle().Foreground(tint(rose, dimmed)).Render("●")
				default:
					want = butterStyle.Render("○")
				}
				got, animated := model.nodeGlyphStyled(node, now, dimmed)
				if got != want || animated != wantAnimated {
					t.Fatalf("%s dimmed=%v frame=%d: %q/%v, want %q/%v",
						node.ID, dimmed, frame, got, animated, want, wantAnimated)
				}
			}
		}
	}
}
