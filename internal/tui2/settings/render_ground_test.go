package settings

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The sheet's own ground (12.11, [tokens.Sheet]).
//
// The boundary was drawn around a panel that painted no floor, so the settings
// sheet and the transcript it floats over shared the terminal's background and
// read as one room. These two tests are the fix stated as a law: every cell of
// the rectangle is the dialog's, and the elevation is the first thing to yield
// where a profile or a mode cannot carry it honestly.

func groundedModel(t *testing.T, profile tokens.Profile, linear bool) *Model {
	t.Helper()
	return New(Options{
		Registry: config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()}),
		Styler:   tokens.NewStyler(profile, tokens.FocusNormal),
		Linear:   linear,
		Debounce: -1,
	})
}

func TestTheSheetPaintsItsWholeRectangle(t *testing.T) {
	m := groundedModel(t, tokens.TrueColor, false)
	const width, height = 80, 24
	ground := tokens.Sheet.Bg(tokens.TrueColor, tokens.FocusNormal)
	band := tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)

	lines := strings.Split(m.Render(width, height), "\n")
	if len(lines) != height {
		t.Fatalf("the sheet drew %d of its %d rows; the rest show the room behind it", len(lines), height)
	}
	for i, line := range lines {
		if strings.Contains(line, band) {
			continue // the selected row carries the band instead
		}
		if !strings.Contains(line, ground) {
			t.Errorf("row %d has no ground under it: %q", i, line)
		}
		if got := blocks.Width(line); got != width {
			t.Errorf("row %d paints %d of %d cells", i, got, width)
		}
	}
}

// TestLinearAndLowProfilesGetNoGround: a background fill is what a screen
// reader cannot see and what 16 colours cannot say honestly
// ([tokens.Profile.SheetGround]). Nothing is lost — the ground carries no
// information, only elevation — which is why it is the part that yields.
func TestLinearAndLowProfilesGetNoGround(t *testing.T) {
	cases := []struct {
		name    string
		profile tokens.Profile
		linear  bool
	}{
		{"linear", tokens.TrueColor, true},
		{"16 colours", tokens.ANSI16, false},
		{"no colour", tokens.NoColor, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := groundedModel(t, c.profile, c.linear).Render(80, 24)
			if seq := tokens.Sheet.Bg(c.profile, tokens.FocusNormal); seq != "" && strings.Contains(out, seq) {
				t.Errorf("a ground was painted where the surface cannot carry one")
			}
			if strings.Contains(out, "settings") == false {
				t.Errorf("the sheet stopped rendering entirely")
			}
		})
	}
}
