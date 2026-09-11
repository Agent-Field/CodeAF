package session

// THE LAW: A CHECK BECOMES A DOOR THROUGH A MAP, NEVER THROUGH A DIRECTORY.
//
// A declared check is written in the folder the work is ABOUT and run in a copy
// of it (task_brief.go's [taskCopy.bindCommand], #886). The defect was a door
// built from a bare directory string: it knew where the checker stood and not
// what that directory was a copy of, so an absolute path in a check read the
// person's own folder while the work sat somewhere else. The fix holds only as
// long as every door is built from a MAP — and a map is one of two things:
//
//   - [Agent.checkCopy], for a checker standing in a copy of a node's ground;
//   - [standingOn], for a checker standing on the folder itself, where there is
//     nothing to bind because the work is already there.
//
// So the check is on the SOURCE. Every call that turns declared checks into a
// door — [auditDoorFor] and [runnableChecks] — hands over one of those two, or
// passes on the map it was itself handed. And the map's fields are spelled in
// one file: a copy constructed anywhere else would be a second reading of "is
// this directory a copy of that ground", which is the drift [copyOnto] exists
// to prevent. The zero map is allowed wherever it stands, because it binds
// nothing and names no directory.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkDoors are the functions that turn declared checks into something a
// checker may run, and mapDoors are the only calls allowed to hand them a map.
var (
	checkDoors = map[string]bool{"auditDoorFor": true, "runnableChecks": true}
	mapDoors   = map[string]bool{"checkCopy": true, "standingOn": true}
)

// copyFile is where a copy's fields may be spelled ([newTaskCopyOf],
// [copyOnto], [standingOn]).
const copyFile = "task_brief.go"

func TestEveryCheckDoorIsBuiltFromAMap(t *testing.T) {
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	doors := 0
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, declared := range file.Decls {
			fn, ok := declared.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// The parameter a door was itself handed may be passed on: that is
			// the same map travelling one call further, not a new one.
			handed := mapParameters(fn)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.CallExpr:
					called := calledName(node.Fun)
					if checkDoors[called] {
						doors++
						if len(node.Args) == 0 || !fromAMap(node.Args[len(node.Args)-1], handed) {
							t.Errorf("%s: %s is handed something that is not a map onto the copy the check runs in; pass checkCopy(...) or standingOn(...) (task_brief.go, #886)",
								fset.Position(node.Pos()), called)
						}
					}
					if (called == "newTaskCopy" || called == "newTaskCopyOf") && name != copyFile {
						t.Errorf("%s: a copy is constructed outside %s; reach it through copyOnto, checkCopy or standingOn so there is one reading of what a directory is a copy of",
							fset.Position(node.Pos()), copyFile)
					}
				case *ast.CompositeLit:
					if ident, ok := node.Type.(*ast.Ident); ok && ident.Name == "taskCopy" && len(node.Elts) > 0 && name != copyFile {
						t.Errorf("%s: a copy's fields are spelled outside %s", fset.Position(node.Pos()), copyFile)
					}
				}
				return true
			})
		}
	}
	// A law that found nothing to judge is a law that stopped looking.
	if doors == 0 {
		t.Fatal("no call builds a check door any more; if the doors were renamed, rename them here")
	}
}

// fromAMap answers whether an argument is one of the two maps, or a map this
// function was handed. The name a call reaches is read the way the taxonomy law
// reads it ([calledName], taxonomy_law_test.go), so the two laws cannot come to
// disagree about what a call is.
func fromAMap(arg ast.Expr, handed map[string]bool) bool {
	switch arg := arg.(type) {
	case *ast.CallExpr:
		return mapDoors[calledName(arg.Fun)]
	case *ast.Ident:
		return handed[arg.Name]
	}
	return false
}

// mapParameters are the names of this function's parameters whose type is a
// copy.
func mapParameters(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	for _, field := range fn.Type.Params.List {
		if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "taskCopy" {
			for _, name := range field.Names {
				out[name.Name] = true
			}
		}
	}
	return out
}
