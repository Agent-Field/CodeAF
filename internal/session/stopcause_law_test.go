package session

// THE LAW: EVERY TURN CANCEL NAMES ITS DOOR.
//
// `a.cancel` is a [context.CancelCauseFunc], so Go already insists that every
// caller passes SOMETHING. What Go cannot insist on is that the something says
// anything: `cancel(nil)` compiles, and a door added later that used it would
// put this package back exactly where it was on 2026-09-09 — a turn ending with
// `context canceled` and no way to tell a person's stop key from a window
// taking the conversation over.
//
// So the check is on the SOURCE. Every cancel of a turn passes `stopFor(...)`,
// with exactly two exemptions named below: the turn's own tidying on the way
// out, where nobody is waiting on the context and a door would put a stop on a
// turn that finished. A third would be a decision somebody makes in a diff.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tidyingCancels is the number of `cancel(nil)` calls this package is allowed:
// one per turn shape that ends by finishing rather than by being stopped. There
// are two turn shapes — the ordinary one in agent.go and the vision turn in
// image.go — and each tidies up after itself exactly once.
var tidyingCancels = map[string]int{
	"agent.go": 1,
	"image.go": 1,
}

func TestEveryTurnCancelNamesItsDoor(t *testing.T) {
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	tidying := map[string]int{}
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
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !cancelsATurn(call.Fun) || len(call.Args) != 1 {
				return true
			}
			named, isName := call.Args[0].(*ast.Ident)
			if !isName || named.Name != "nil" {
				return true
			}
			tidying[name]++
			if tidying[name] > tidyingCancels[name] {
				t.Errorf("%s: a turn is cancelled with no door named; pass stopFor(<door>) so the ending can account for itself (stopcause.go)",
					fset.Position(call.Pos()))
			}
			return true
		})
	}
	for name, want := range tidyingCancels {
		if got := tidying[name]; got != want {
			t.Errorf("%s tidies up after %d turn(s), the law expects %d — if a turn shape moved, move the exemption with it",
				name, got, want)
		}
	}
}

// cancelsATurn reports whether this call expression is one of the turn's own
// cancellation handles. It is a NAME test rather than a type test because the
// law is about a convention the reader keeps, and the names in this package are
// `cancel` (the local the turn mints) and `a.cancel` (the field it installs).
func cancelsATurn(fun ast.Expr) bool {
	switch called := fun.(type) {
	case *ast.Ident:
		return called.Name == "cancel"
	case *ast.SelectorExpr:
		return called.Sel.Name == "cancel"
	}
	return false
}
