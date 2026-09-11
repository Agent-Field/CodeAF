package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ── THE LAW: ONE FEEDER, AND IT IS THE ONE THE READINGS ALREADY PASS ────────
//
// A seam that tells a surface how a call is going can be fed from anywhere —
// the SSE decoder, the answer splitter, the phase clock, a counter of its own —
// and every one of those would be a SECOND COUNT of a thing that is already
// counted. The census of 2026-09-10 is the worked example of what that costs in
// this package: three separate rules deciding what a 404 meant, one row saying
// a call took ten seconds and another saying nine hundred, and nobody able to
// say which was the build's real opinion.
//
// So [callProgress] is reached from ONE place — the watch every streamed
// reading is already folded into (armwatch.go) — and the two ends of a request
// are taken from the two rows the model-call log already writes. This test
// fails the build on the day a second feeder appears, which is the day a
// surface could be shown two different token counts for one call.
func TestTheCallProgressSeamHasOneFeeder(t *testing.T) {
	// reached names the functions that can get at the seam at all, and the one
	// file each may be called from. Everything else must go through a
	// [streamWatch] forwarder, which is what keeps the counts single.
	reached := map[string]string{
		"callProgressFrom": "armwatch.go",
		"newCallProgress":  "armwatch.go",
	}
	// forwarded names the watch's own doors onto the seam and the files that may
	// drive them. They are the two ends of a request and the pacing park, and
	// each of the three is a fact only that file holds.
	forwarded := map[string]map[string]bool{
		"callOpened": {"calllog.go": true},
		"callClosed": {"calllog.go": true},
		"callPaced":  {"dispatch.go": true},
	}

	callers := map[string][]string{}
	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				callers[fun.Name] = append(callers[fun.Name], name)
			case *ast.SelectorExpr:
				callers[fun.Sel.Name] = append(callers[fun.Sel.Name], name)
			}
			return true
		})
	}

	for reader, only := range reached {
		seen := callers[reader]
		if len(seen) != 1 {
			sort.Strings(seen)
			t.Fatalf("%s is called from %v, want exactly one place — the watch every reading already passes", reader, seen)
		}
		if seen[0] != only {
			t.Fatalf("%s is called from %s, want %s", reader, seen[0], only)
		}
	}
	for door, allowed := range forwarded {
		seen := callers[door]
		if len(seen) == 0 {
			t.Fatalf("%s is called from nowhere, so the seam is silent about what it is for", door)
		}
		for _, from := range seen {
			if from == "armwatch.go" {
				// The forwarder's own declaration file; a method named here is
				// the door itself, not a second driver of it.
				continue
			}
			if !allowed[from] {
				sort.Strings(seen)
				t.Fatalf("%s is called from %s, want only %v — a second driver is a second account of when a call began", door, from, keysOf(allowed))
			}
		}
	}
}
