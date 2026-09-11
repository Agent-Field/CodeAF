package furrow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// personPaced are the only doors onto furrow that may run it WITHOUT A BOUND OF
// THEIR OWN, each with the reason it is the person's clock and not this
// package's. Everything else that runs the program has a bound, because a
// furrow that never answers is otherwise a caller that never returns — and one
// of those callers is a task waiting to start.
var personPaced = map[string]string{
	// The command inside the universe is the person's (or the model's) own, and
	// the caller's context is what carries their decision to stop.
	"RunInFork": "runs a command the caller chose, for as long as it takes",
	// The two plumbing functions are the seam itself, not a door onto it: the
	// bound belongs to whoever calls them.
	"run":       "the method every door calls through",
	"runBinary": "the one exec every door ends in",
}

// THE LAW: EVERY CALL INTO FURROW IS BOUNDED, OR IS NAMED ABOVE AS THE PERSON'S.
//
// Workspace.Fork was the call that broke it. Every read beside it had a bound
// and it had none, so a furrow that hung while forking a workspace hung the
// start of a task with nothing anywhere able to cut it. A door that runs the
// program must either take a deadline in its own body — conditionally is fine,
// the restore bounds only its preview — or be listed in [personPaced] with the
// reason its clock is somebody else's.
func TestEveryCallIntoFurrowIsBounded(t *testing.T) {
	files := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if _, exempt := personPaced[fn.Name.Name]; exempt {
				continue
			}
			runs, bounded := false, false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch callee := call.Fun.(type) {
				case *ast.Ident:
					if callee.Name == "runBinary" {
						runs = true
					}
				case *ast.SelectorExpr:
					if callee.Sel.Name == "run" {
						runs = true
					}
					if pkg, ok := callee.X.(*ast.Ident); ok && pkg.Name == "context" &&
						(callee.Sel.Name == "WithTimeout" || callee.Sel.Name == "WithDeadline") {
						bounded = true
					}
				}
				return true
			})
			if runs && !bounded {
				t.Errorf("%s: %s runs furrow with no bound of its own; bound it with context.WithTimeout, or name it in personPaced with the reason its clock is the person's",
					files.Position(fn.Pos()), fn.Name.Name)
			}
		}
	}
}
