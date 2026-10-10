package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The operating-system timers an older build installed run `<binary> wake`
// every five minutes, with the flags that build gave them, until this build's
// first start removes them. Every one of those runs has to be a quiet success:
// an exit status of 1 is what a timer reports as a failure, every five
// minutes, to whoever reads the system log — and run() hands a nil straight
// back to the one exit as status 0.
func TestWakeIsAQuietSuccessWhateverAnOlderTimerPasses(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--db", filepath.Join(t.TempDir(), "graph.db")},
		{"--db", "/nowhere/graph.db", "--timeout", "2m"},
		{"--max-seconds", "120"},
		{"--a-flag-nobody-ever-wrote"},
	} {
		if err := runWake(args); err != nil {
			t.Fatalf("codeaf wake %s = %v, want a quiet success", strings.Join(args, " "), err)
		}
	}
}
