package main

import (
	"strings"
	"testing"
	"time"
)

// A slow part of the wrap-up is named on stderr with how long it took, and a
// quick one says nothing.
func TestLeavingNamesTheSlowPart(t *testing.T) {
	at := time.Date(2026, 11, 8, 3, 29, 20, 0, time.UTC)
	var out strings.Builder
	lap := leaveLapsTo(&out, func() time.Time { return at })
	at = at.Add(200 * time.Millisecond)
	lap("chats")
	at = at.Add(40 * time.Second)
	lap("local factory")
	at = at.Add(300 * time.Millisecond)
	lap("conversations")
	if got, want := out.String(), "codeaf app: leaving: local factory took 40s\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
