package consentui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The dialog's own ground (12.11, [tokens.Sheet]).
//
// 12.11 gave a floating dialog "a one-cell margin of its own ground" and then
// painted neither the margin nor the panel, so the dialog and the room it floats
// over shared the terminal's background. A consent dialog is the surface where
// that matters most: it is asking for a decision, and a decision surface that
// does not read as a separate plane reads as part of the transcript that
// prompted it.

func groundedDialog(t *testing.T, profile tokens.Profile, linear bool) *Model {
	t.Helper()
	m := New(Options{
		Styler:   tokens.NewStyler(profile, tokens.FocusNormal),
		Linear:   linear,
		OnAnswer: func(Result) tea.Cmd { return nil },
		OnClose:  func() tea.Cmd { return nil },
	})
	m.Focus(true)
	m.Push(consentQuestion())
	return m
}

func TestTheDialogPaintsItsWholeRectangle(t *testing.T) {
	m := groundedDialog(t, tokens.TrueColor, false)
	const width, height = 70, 18
	ground := tokens.Sheet.Bg(tokens.TrueColor, tokens.FocusNormal)
	band := tokens.Band.Bg(tokens.TrueColor, tokens.FocusNormal)

	lines := strings.Split(m.Render(width, height), "\n")
	if len(lines) != height {
		t.Fatalf("the dialog drew %d of its %d rows; the rest show the room behind it", len(lines), height)
	}
	for i, line := range lines {
		if strings.Contains(line, band) {
			continue // the selected option carries the band instead
		}
		if !strings.Contains(line, ground) {
			t.Errorf("row %d has no ground under it: %q", i, line)
		}
		if got := blocks.Width(line); got != width {
			t.Errorf("row %d paints %d of %d cells", i, got, width)
		}
	}
}

// TestNoGroundWhereItCannotBeCarried: the same four states that forbid the
// selection band forbid the ground, and for the same reasons — see
// [Model.grounded]. The dialog keeps every word either way; only the elevation
// yields.
func TestNoGroundWhereItCannotBeCarried(t *testing.T) {
	cases := []struct {
		name    string
		profile tokens.Profile
		linear  bool
		blur    bool
	}{
		{"linear", tokens.TrueColor, true, false},
		{"16 colours", tokens.ANSI16, false, false},
		{"no colour", tokens.NoColor, false, false},
		{"unfocused", tokens.TrueColor, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := groundedDialog(t, c.profile, c.linear)
			if c.blur {
				m.Focus(false)
			}
			out := m.Render(70, 18)
			if seq := tokens.Sheet.Bg(c.profile, tokens.FocusNormal); seq != "" && strings.Contains(out, seq) {
				t.Error("a ground was painted where the surface cannot carry one")
			}
			if !strings.Contains(plain(out), "consent") {
				t.Errorf("the dialog stopped saying what it is:\n%s", plain(out))
			}
		})
	}
}
