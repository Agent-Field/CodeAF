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
// this process, and recordNode and recordRoot settle adaptive-run rows.
var liveLandingEndedAtWriters = map[string]string{
	"recordTaskIndex":            "a node has just landed through the graph's report hook",
	"closeInflightTaskIndexRows": "this process is closing a row whose live owner is gone",
	"recordNode":                 "an adaptive run's node has just landed",
	"recordRoot":                 "an adaptive run's root has just landed",
}

// C8 — Every session writer that stamps a TaskIndexEntry with time.Now is a
// live landing transition. In particular, rebuilding a row from a node is a
// read of history and may never consult the wall clock.
func TestOnlyALiveLandingStampsARowWithNow(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/session: %v", err)
	}
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
							if containsTimeNow(value) {
								assertLiveLandingWriter(t, liveLandingEndedAtWriters, functionName, name, fset.Position(value.Pos()).Line)
							}
						}
					}
				case *ast.KeyValueExpr:
					key, ok := typed.Key.(*ast.Ident)
					if ok && key.Name == "EndedAt" && containsTimeNow(typed.Value) {
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
