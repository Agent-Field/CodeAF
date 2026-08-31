package provider

import (
	"os"
	"strings"
	"testing"
)

// ── NO FETCH ON THE SEND PATH ───────────────────────────────────────────────
//
// This whole package is the send path. Everything in it runs either while a
// request is being shaped — the encoder reads the routing preference at the
// last possible moment, deliberately, so that a demotion earned thirty seconds
// ago applies to the request being written now — or while a stream is being
// read, one delta at a time, against the connection's own idle watchdog.
//
// The lane sheet (`internal/lane`) has two halves for exactly this reason:
// Rows reads memory and may be called from anywhere, and Refresh goes to the
// network and belongs to a background beat. A Refresh reached from here would
// put an HTTP round trip in front of a person's first token, and it would do it
// invisibly — the call would succeed, the answer would be right, and the
// surface would simply have felt slower since some Tuesday.
//
// The home-lag investigation of 2026-08-30 is the same lesson in another
// package: one reading per beat, never a reading per keystroke. This test is
// the version of it the build can enforce, and it is a text scan rather than an
// AST walk on purpose — a method value, an interface embedded in a struct, and
// a wrapper named RefreshLanes are all things a reader would call a fetch, and
// all of them would slip past a check for one particular call expression.
func TestNothingOnTheSendPathRefreshesASheet(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for number, line := range strings.Split(string(source), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, "Refresh(") {
				t.Errorf("%s:%d fetches a sheet on the send path: %s",
					name, number+1, strings.TrimSpace(line))
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no sources were scanned, so this law passed vacuously")
	}
}
