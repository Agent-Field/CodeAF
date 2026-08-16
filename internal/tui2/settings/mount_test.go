package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
)

// The mounting contract, driven through the real shell rather than asserted in
// a comment. 8.2.19 calls the sheet full-screen and 13.2's P2 wants a settings
// entry in the new shell even before the rest of Wave 4 lands, so this package
// has to be bindable BOTH ways — raised on the overlay plane, or standing in
// as the main pane — without knowing which one it got.

func mountable(t *testing.T) *Model {
	t.Helper()
	return New(Options{
		Registry: config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()}),
		Debounce: -1,
	})
}

func TestMountsOnTheOverlayPlaneAndOnTheMainPane(t *testing.T) {
	for _, mount := range []struct {
		name    string
		layer   tui2.LayerID
		overlay bool
	}{
		{"overlay", tui2.LayerOverlay, true},
		{"main pane", tui2.LayerTranscript, false},
	} {
		t.Run(mount.name, func(t *testing.T) {
			shell := tui2.NewShell(tui2.Options{})
			pane := mountable(t)
			shell.SetPane(mount.layer, pane)
			shell.SetOverlay(mount.overlay)

			frame := shell.Frame(96, 30)
			if !strings.Contains(frame, "settings") {
				t.Fatalf("the sheet did not reach the frame:\n%s", frame)
			}
			// Whatever the shell hands it, the pane fills and never overruns.
			for _, line := range strings.Split(frame, "\n") {
				if len([]rune(line)) > 96*8 {
					t.Fatalf("a frame line is implausibly long: %d bytes", len(line))
				}
			}
		})
	}
}

// The shell routes keys to the focused pane and esc is answered by this one,
// every time — it never leaks past the sheet into whatever is underneath.
func TestEscIsAlwaysConsumedByTheFocusedSheet(t *testing.T) {
	closed := 0
	shell := tui2.NewShell(tui2.Options{})
	pane := New(Options{
		Registry: config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()}),
		OnClose:  func() tea.Cmd { closed++; return nil },
		Debounce: -1,
	})
	shell.SetPane(tui2.LayerTranscript, pane)
	shell.Frame(96, 30)

	shell.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if pane.Query() != "b" {
		t.Fatalf("the shell did not route typing to the focused sheet (query %q)", pane.Query())
	}
	shell.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if pane.Query() != "" || closed != 0 {
		t.Fatalf("esc did not clear the search first (query %q, closed %d)", pane.Query(), closed)
	}
	shell.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if closed != 1 {
		t.Fatalf("esc closed %d times, want once", closed)
	}
}

// Focus is a property of the pane (8.3): losing it dims, and dimming must not
// change what the sheet says.
func TestLosingFocusDimsWithoutChangingTheWords(t *testing.T) {
	pane := mountable(t)
	before := pane.Render(80, 20)
	pane.Focus(false)
	after := pane.Render(80, 20)
	if before != after {
		t.Fatal("an unstyled sheet changed its bytes on a focus change")
	}
	pane.Focus(true)
	if pane.Render(80, 20) != before {
		t.Fatal("focus did not return the sheet to where it started")
	}
}
