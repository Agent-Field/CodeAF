package tui3

import (
	"strings"
	"testing"
)

// C15 and C26 require slash help to explain enabling unavailable chat pairing.
func TestPairHelpWhenMovingChatsIsOff(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "")
	c := command{name: "pair", desc: "show a code"}
	if got := c.note(chordSpelling{}); !strings.Contains(got, "CODEAF_CELLS=1") {
		t.Fatalf("pair help: %s", got)
	}
}
