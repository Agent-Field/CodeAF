package tui3

import (
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// C27.1 and C27.6 require ordinary pairing help without an enable instruction.
// The platform reason stays available where moving chats cannot work (C27.3).
func TestPairHelpByDefault(t *testing.T) {
	c := command{name: "pair", desc: "show a code"}
	got := c.note(chordSpelling{})
	if runtime.GOOS == "windows" {
		if !strings.Contains(got, cell.WindowsReason) {
			t.Fatalf("pair help: %s", got)
		}
	} else if got != "show a code" {
		t.Fatalf("pair help: %s", got)
	}
}
