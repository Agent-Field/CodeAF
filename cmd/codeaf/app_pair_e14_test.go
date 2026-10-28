package main

import (
	"strings"
	"testing"
)

func TestA3PairingInstructionsNameDesktopControls(t *testing.T) {
	want := "On your other computer, choose Pair a computer, then Enter a code from the other computer, and type 42-715-302. This code lasts 10 minutes."
	if got := appPairCodeLines("42-715-302")[1]; got != want {
		t.Fatalf("A3: instructions = %q, want %q", got, want)
	}
	if got := strings.Join(appPairCodeLines("42-715-302"), " "); strings.Contains(got, "Join another computer") {
		t.Fatal("A3: obsolete desktop control")
	}
}
