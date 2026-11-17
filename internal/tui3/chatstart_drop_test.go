package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A FILE DROPPED ON THE START PAGE IS SHOWN ON THE START PAGE. The drop attaches
// at once (the tray is the start page's own), but the page draws its box
// through its own unit, which used to leave the tray row out. The person saw
// nothing land, dropped the file again, and found it attached twice once the
// conversation opened.
func TestAFileDroppedOnTheStartPageIsShownThere(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	openStart(t, a)

	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.paste(path)

	if len(a.chips) != 1 {
		t.Fatalf("the drop did not reach the start page's tray: %d chips", len(a.chips))
	}
	width, _ := a.size()
	shown := plain(strings.Join(a.welcomeRows(width), "\n"))
	if !strings.Contains(shown, "shot.png") {
		t.Fatalf("the start page does not show the dropped file; rows:\n%s", shown)
	}
}
