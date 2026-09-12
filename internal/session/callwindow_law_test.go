package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// ── THE LAW: EVERY CHECKER CALL IS SENT UNDER A WINDOW IT IS TOLD ──────────
//
// The checker's call was bounded by a bare context.WithTimeout, which is a
// stopwatch the model never sees, and #941 is what that cost: correct work
// landed `your call` because a reasoning model thought through both of its
// shares without a word. The fix is three shapes, and each is one line somebody
// could quietly undo — so each is read off the source here, rather than hoped
// for:
//
//   - every request put to a checker in task_audit.go goes out on a context the
//     same function opened with [openCallWindow], and nothing in that file bounds
//     a checker's call any other way;
//   - the one client door hands the completer the context [toldItsWindow]
//     returns, which is the only place the window becomes a wall on the wire;
//   - every checking window is opened from [Agent.auditWindowFor], the one place
//     its length is decided.
func TestEveryCheckerCallIsSentUnderAToldWindow(t *testing.T) {
	fset := token.NewFileSet()
	parse := func(name string) *ast.File {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		return file
	}

	audit := parse("task_audit.go")
	submits := 0
	for _, decl := range audit.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// The contexts this function opened as told windows, by name.
		opened := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Rhs) != 1 || len(assign.Lhs) == 0 {
				return true
			}
			if call, ok := assign.Rhs[0].(*ast.CallExpr); ok && calledName(call.Fun) == "openCallWindow" {
				if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
					opened[ident.Name] = true
				}
			}
			return true
		})
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch calledName(call.Fun) {
			case "WithTimeout", "WithDeadline":
				if pkg, ok := call.Fun.(*ast.SelectorExpr); ok {
					if ident, ok := pkg.X.(*ast.Ident); ok && ident.Name == "context" {
						t.Errorf("%s: %s bounds a call with context.%s — a checker's call is bounded by a window it is told (openCallWindow)",
							fset.Position(call.Pos()), fn.Name.Name, calledName(call.Fun))
					}
				}
			case "Submit":
				submits++
				if len(call.Args) == 0 {
					return true
				}
				ident, ok := call.Args[0].(*ast.Ident)
				if !ok || !opened[ident.Name] {
					t.Errorf("%s: %s puts a request to a checker on a context it did not open with openCallWindow",
						fset.Position(call.Pos()), fn.Name.Name)
				}
			}
			return true
		})
	}
	if submits == 0 {
		t.Fatal("task_audit.go puts no request to a checker at all; the law above passed by reading nothing")
	}

	door := parse("clientdoor.go")
	toldAtTheDoor := false
	for _, decl := range door.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "completeWithModel" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || calledName(call.Fun) != "CompleteWithMessages" || len(call.Args) == 0 {
				return true
			}
			if told, ok := call.Args[0].(*ast.CallExpr); ok && calledName(told.Fun) == "toldItsWindow" {
				toldAtTheDoor = true
			}
			return true
		})
	}
	if !toldAtTheDoor {
		t.Error("clientdoor.go's completeWithModel no longer hands the completer toldItsWindow(ctx); " +
			"an opened window would then bound the call and tell the model nothing")
	}

	paces := 0
	for name, file := range parsedPackage(t, fset) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || calledName(call.Fun) != "newAuditPace" || len(call.Args) == 0 {
				return true
			}
			paces++
			if window, ok := call.Args[0].(*ast.CallExpr); !ok || calledName(window.Fun) != "auditWindowFor" {
				t.Errorf("%s: a checking window is opened from something other than auditWindowFor (%s)",
					fset.Position(call.Pos()), name)
			}
			return true
		})
	}
	if paces == 0 {
		t.Fatal("no checking window is opened anywhere; the law above passed by reading nothing")
	}
}

// parsedPackage is every non-test file of this package, parsed, by name.
func parsedPackage(t *testing.T, fset *token.FileSet) map[string]*ast.File {
	t.Helper()
	files := map[string]*ast.File{}
	for name := range packageSourceText(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files[name] = file
	}
	return files
}
