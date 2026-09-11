package session

// THE SOURCE LAW FOR THE QUICK TASK'S ONE DOOR LIVES ALONE HERE. scripts/laws.sh
// runs every test in a file that imports go/ast or go/parser, so this file holds
// only the structural check; what the door does is task_quick_door_test.go's.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// quickBodyWriters names every function allowed to put a quick body on a spec,
// and what each one is. There are two, and they are the only two moments a quick
// node's body can come into being: admitted, and read back off the record.
var quickBodyWriters = map[string]string{
	"newQuickSpec": "the one admission door's spec — every road's quick node is built here",
	"restoreNode":  "the record read back — the body the checkpoint carried, rebuilt",
}

// quickGateHolders names every function allowed to take the admission gate.
var quickGateHolders = map[string]string{
	"admitQuick": "the one door, which holds the look at the claims and the admit as one move",
}

// ONE DOOR, AND THE SOURCE SAYS SO.
//
// The ceiling's handover once built its own quick node and admitted it through
// the route judge's launcher, and what came out was a different object from the
// tool's: an invented done-condition, a working copy's mode, a naming call, no
// family and no look at the write claims. [TestTheCeilingsQuickNodeHasTheToolsShape]
// catches that for the two roads that exist; this catches the third road before
// it is written. A `quick:` field in a spec literal, an assignment to a spec's
// `quick`, a body literal outside its constructor, or the admission gate taken
// anywhere but the door — each is a second door, and each fails here by name.
func TestAQuickNodeIsBuiltAndAdmittedOnlyThroughItsOneDoor(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/session: %v", err)
	}
	seen := map[string]bool{}
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
			owner := function.Name.Name
			at := func(node ast.Node) string {
				return name + ":" + strconv.Itoa(fset.Position(node.Pos()).Line) + ": " + owner
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CompositeLit:
					switch typeName(typed.Type) {
					case "taskSpec":
						if literalSets(typed, "quick") {
							seen[owner] = true
							if _, allowed := quickBodyWriters[owner]; !allowed {
								t.Errorf("%s builds a spec carrying a quick body; every quick node is built by newQuickSpec and admitted by admitQuick", at(typed))
							}
						}
					case "quickTaskSpec":
						if owner != "newQuickTaskSpec" {
							t.Errorf("%s assembles a quick body by hand; newQuickTaskSpec is the one constructor, and it is what keeps the ticks parallel to the items", at(typed))
						}
					}
				case *ast.AssignStmt:
					for _, target := range typed.Lhs {
						if selector, ok := target.(*ast.SelectorExpr); ok && selector.Sel.Name == "quick" {
							t.Errorf("%s assigns a spec's quick body after it was built; a quick spec comes whole out of newQuickSpec", at(typed))
						}
					}
				case *ast.SelectorExpr:
					if typed.Sel.Name == "quickGate" {
						if _, allowed := quickGateHolders[owner]; !allowed {
							t.Errorf("%s takes the quick admission gate; admitQuick is the only door that holds it", at(typed))
						}
					}
				}
				return true
			})
		}
	}
	// A PERMISSION NOTHING USES IS A PERMISSION NOBODY NOTICED GOING STALE.
	for writer := range quickBodyWriters {
		if !seen[writer] {
			t.Errorf("%s is allowed to build a quick spec and no longer does; delete its permission", writer)
		}
	}
}

// typeName is a composite literal's type as written — `taskSpec` for both
// `taskSpec{…}` and a pointer to one.
func typeName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return typeName(typed.X)
	}
	return ""
}

// literalSets reports whether a keyed composite literal sets the named field.
func literalSets(literal *ast.CompositeLit, field string) bool {
	for _, element := range literal.Elts {
		if pair, ok := element.(*ast.KeyValueExpr); ok {
			if key, ok := pair.Key.(*ast.Ident); ok && key.Name == field {
				return true
			}
		}
	}
	return false
}
