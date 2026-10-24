package tui3

import "testing"

// TestPairHelpByDefault proves C27.1, C27.6 and K1: pairing has ordinary help
// on Windows as on Unix, without an enable instruction or a platform refusal.
func TestPairHelpByDefault(t *testing.T) {
	c := command{name: "pair", desc: "show a code"}
	if got := c.note(chordSpelling{}); got != "show a code" {
		t.Fatalf("pair help: %s", got)
	}
}
