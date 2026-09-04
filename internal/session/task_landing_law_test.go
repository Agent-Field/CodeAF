package session

// THE SOURCE LAW FOR TASK LANDING STAMPS LIVES ALONE HERE. scripts/laws.sh runs
// every test in a file that imports go/ast or go/parser, so this file contains
// only the structural check and its helpers; graph behavior remains in
// task_landing_stamp_test.go and off the fast law gate.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// liveLandingEndedAtWriters names every function allowed to derive a row's
// EndedAt from time.Now. Each is a LIVE LANDING TRANSITION: recordTaskIndex is
// the node landing backstop, closeInflightTaskIndexRows closes work abandoned by
// this process, recordNode and recordRoot settle adaptive-run rows, and
// interrupt settles the two kinds for which a process exit IS an ending.
var liveLandingEndedAtWriters = map[string]string{
	"recordTaskIndex":            "a node has just landed through the graph's report hook",
	"closeInflightTaskIndexRows": "this process is closing a row whose live owner is gone",
	"recordNode":                 "an adaptive run's node has just landed",
	"recordRoot":                 "an adaptive run's root has just landed",
	"interrupt":                  "a design or run that settles on interruption is ending here",
	// AN ADAPTIVE FAMILY'S OPENING ROW IS THE ODD ONE and it is listed rather
	// than excused: it writes `EndedAt: family.started` on a row whose Status is
	// TaskRunning, so a run that has not ended carries its START instant in the
	// ending field, to give the row something to sort and date by while it runs.
	// Reasonable on the tasks place, confusing everywhere else, and the widened
	// law is what surfaced it — it is older than this change and out of its
	// scope to move.
	"newOrchestrateFamily": "an adaptive family's opening row dates itself by its start",
}

// C8 — Every session writer that stamps a TaskIndexEntry with time.Now is a
// live landing transition. In particular, rebuilding a row from a node is a
// read of history and may never consult the wall clock.
func TestOnlyALiveLandingStampsARowWithNow(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/session: %v", err)
	}
	// THE CLOCK IS FOUND THROUGH A NAME AS WELL AS THROUGH A CALL. The law read
	// only `time.Now()` written out at the assignment, so `now := time.Now()`
	// two lines above it, or a helper that returns one, walked straight past —
	// and closeInflightTaskIndexRows was already doing exactly that, which made
	// its entry above dead weight rather than a permission. Both forms are
	// found now: every function whose body reaches the clock is collected
	// first, and every local bound to one is collected per function.
	clockFunctions := functionsReachingTheClock(t, entries)

	foundIndexBuilder := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			functionName := function.Name.Name
			clockLocals := localsBoundToTheClock(function.Body, clockFunctions)
			if functionName == "indexEntryLocked" {
				foundIndexBuilder = true
				if containsTimeNow(function.Body) {
					t.Errorf("%s:%d: indexEntryLocked reads time.Now while rebuilding a row from the record", name, fset.Position(function.Pos()).Line)
				}
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.AssignStmt:
					for at, target := range typed.Lhs {
						if !isEndedAtTarget(target) {
							continue
						}
						values := typed.Rhs
						if len(typed.Lhs) == len(typed.Rhs) {
							values = typed.Rhs[at : at+1]
						}
						for _, value := range values {
							if readsTheClock(value, clockFunctions, clockLocals) {
								assertLiveLandingWriter(t, liveLandingEndedAtWriters, functionName, name, fset.Position(value.Pos()).Line)
							}
						}
					}
				case *ast.KeyValueExpr:
					key, ok := typed.Key.(*ast.Ident)
					if ok && key.Name == "EndedAt" && readsTheClock(typed.Value, clockFunctions, clockLocals) {
						assertLiveLandingWriter(t, liveLandingEndedAtWriters, functionName, name, fset.Position(typed.Value.Pos()).Line)
					}
				}
				return true
			})
		}
	}
	if !foundIndexBuilder {
		t.Fatal("indexEntryLocked was not found; the law did not inspect the row builder")
	}
}

// functionsReachingTheClock is every function in this package whose body reads
// time.Now, so a stamp written through a helper is still a stamp.
func functionsReachingTheClock(t *testing.T, entries []os.DirEntry) map[string]bool {
	t.Helper()
	reaching := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			if containsTimeNow(function.Body) {
				reaching[function.Name.Name] = true
			}
		}
	}
	return reaching
}

// localsBoundToTheClock is every name in one function bound to a reading of the
// clock, directly or through one of the helpers that reaches it.
func localsBoundToTheClock(body *ast.BlockStmt, clockFunctions map[string]bool) map[string]bool {
	bound := map[string]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for at, target := range assignment.Lhs {
			named, ok := target.(*ast.Ident)
			if !ok {
				continue
			}
			values := assignment.Rhs
			if len(assignment.Lhs) == len(assignment.Rhs) {
				values = assignment.Rhs[at : at+1]
			}
			for _, value := range values {
				if readsTheClock(value, clockFunctions, nil) {
					bound[named.Name] = true
				}
			}
		}
		return true
	})
	return bound
}

// readsTheClock answers whether an expression is a reading of the wall clock —
// written out, held in a local, or returned by a helper.
//
// IT ERRS TOWARD CATCHING. A local bound to a composite literal that reads the
// clock anywhere inside it counts as a clock local, so `family.started` off a
// struct built with `started: time.Now()` is found — and so would a field of
// that struct which is not a clock reading at all. That is the right bias for a
// law: a false catch is one line in the map above with a reason beside it, and
// a missed one is a row dated by whoever happened to read it.
func readsTheClock(value ast.Expr, clockFunctions, clockLocals map[string]bool) bool {
	if containsTimeNow(value) {
		return true
	}
	found := false
	ast.Inspect(value, func(inner ast.Node) bool {
		switch typed := inner.(type) {
		case *ast.Ident:
			if clockLocals[typed.Name] {
				found = true
			}
		case *ast.CallExpr:
			if named, ok := typed.Fun.(*ast.Ident); ok && clockFunctions[named.Name] {
				found = true
			}
		}
		return !found
	})
	return found
}

func isEndedAtTarget(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "EndedAt"
}

func containsTimeNow(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		call, ok := inner.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Now" {
			return true
		}
		qualifier, ok := selector.X.(*ast.Ident)
		if ok && qualifier.Name == "time" {
			found = true
			return false
		}
		return true
	})
	return found
}

func assertLiveLandingWriter(t *testing.T, allowed map[string]string, function, file string, line int) {
	t.Helper()
	if _, ok := allowed[function]; ok {
		return
	}
	t.Errorf("%s:%d: %s stamps TaskIndexEntry.EndedAt with time.Now but is not a live landing transition", file, line, function)
}

// TestTheLandingLawFindsTheClockThroughANameAndAHelper pins the widening
// itself, because a law that cannot be shown to catch is a law nobody can
// trust.
//
// The narrow reading — `time.Now()` written out at the assignment — passed
// `now := time.Now(); row.EndedAt = now` and `row.EndedAt = stamp()` straight
// through, and closeInflightTaskIndexRows was already the first of those. Its
// entry in [liveLandingEndedAtWriters] was therefore permission for something
// the law never asked about.
func TestTheLandingLawFindsTheClockThroughANameAndAHelper(t *testing.T) {
	const source = `package session

import "time"

func stamp() time.Time { return time.Now() }

func writtenOut(row TaskIndexEntry)  { row.EndedAt = time.Now() }
func throughUTC(row TaskIndexEntry)  { row.EndedAt = time.Now().UTC() }
func throughAName(row TaskIndexEntry) {
	now := time.Now()
	row.EndedAt = now
}
func throughAHelper(row TaskIndexEntry) { row.EndedAt = stamp() }
func fromTheRecord(row TaskIndexEntry, record taskRecord) { row.EndedAt = record.EndedAt }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "clockforms.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}

	clockFunctions := map[string]bool{"stamp": true}
	caught := map[string]bool{}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		locals := localsBoundToTheClock(function.Body, clockFunctions)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, target := range assignment.Lhs {
				if !isEndedAtTarget(target) {
					continue
				}
				for _, value := range assignment.Rhs {
					if readsTheClock(value, clockFunctions, locals) {
						caught[function.Name.Name] = true
					}
				}
			}
			return true
		})
	}

	for _, form := range []string{"writtenOut", "throughUTC", "throughAName", "throughAHelper"} {
		if !caught[form] {
			t.Errorf("the law does not find the clock in %s, so that spelling walks past it", form)
		}
	}
	// AND IT DOES NOT CATCH THE READING THIS WHOLE CHANGE IS FOR. A row stamped
	// from the record is the correct shape and must stay legal.
	if caught["fromTheRecord"] {
		t.Error("the law refuses a row stamped from the record, which is the shape this change installs")
	}
}
