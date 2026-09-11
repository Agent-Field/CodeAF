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
		// The watcher is read and the question's reporter built in ONE place —
		// the door the question itself returns through, which is also the only
		// thing that can say when it is over (callprogress.go's
		// [beginCallProgress], called from client.go).
		"beginCallProgress": "client.go",
		// And the arms find the reporter that door opened, in the one line that
		// has both the caller's context and the arm about to run from it.
		"callProgressOn": "armwatch.go",
	}
	// reachesIn is the file that may say `….progress.<anything>` — reach THROUGH
	// the reporter's field on [streamWatch] and speak to it directly.
	//
	// IT IS THE HOLE THE FIRST DRAFT OF THIS LAW LEFT, AND THIS PULL REQUEST FELL
	// IN IT. `progress` is a field on the watch and `streamWatchFrom(ctx)` is
	// callable from every file here, so `streamWatchFrom(ctx).progress.note(…)`
	// in client.go is a second feeder that a law naming only the doors would wave
	// through — and the first draft of this change did exactly that from
	// `lostRace`, bypassing its own `callClosed`. Naming the METHODS instead does
	// not work either: `opened` and `paced` are both spelled on other types in
	// this package, and a law that fires on a name rather than on a path is a law
	// somebody deletes the next time it is wrong.
	//
	// So the shape is the rule: a selector chain THROUGH `progress` belongs to
	// the file that owns the field, and every other file goes through a door.
	const reachesIn = "armwatch.go"
	// forwarded names the watch's own doors onto the seam and the files that may
	// drive them. An attempt's two ends come from the model-call log's only
	// writer, the pacing park from the one site in the dispatcher that flips it,
	// and THE CALL BEING OVER from the door a request returns through — the only
	// thing in the process that knows no further attempt is coming.
	forwarded := map[string]map[string]bool{
		"callOpened": {"calllog.go": true},
		"callLanded": {"calllog.go": true},
		"callPaced":  {"dispatch.go": true},
	}
	// finished is the question's ending and is said by the door that opened the
	// report, so it is held to that one file rather than reached through an arm.
	ending := map[string]bool{"callprogress.go": true, "client.go": true}

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
			if selector, ok := node.(*ast.SelectorExpr); ok {
				// `x.progress.y` — the chain that reaches past the doors.
				if through, ok := selector.X.(*ast.SelectorExpr); ok && through.Sel.Name == "progress" && name != reachesIn {
					t.Errorf("%s says .progress.%s — a second feeder of the call-progress seam; go through a %s door",
						name, selector.Sel.Name, reachesIn)
				}
			}
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
	for _, from := range callers["finished"] {
		if !ending[from] {
			t.Fatalf("finished is called from %s, want only %v — a question is over once and only its own door knows when", from, keysOf(ending))
		}
	}
	for door, allowed := range forwarded {
		seen := callers[door]
		if len(seen) == 0 {
			t.Fatalf("%s is called from nowhere, so the seam is silent about what it is for", door)
		}
		for _, from := range seen {
			if from == reachesIn {
				// The forwarder's own declaration file; a method named here is
				// the door itself, not a second driver of it.
				continue
			}
			if !allowed[from] {
				sort.Strings(seen)
				t.Fatalf("%s is called from %s, want only %v — a second driver is a second account of when a request went out", door, from, keysOf(allowed))
			}
		}
	}
}
