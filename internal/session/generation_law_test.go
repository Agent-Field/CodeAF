package session

// THE LAW: A GENERATION IS CUT THROUGH ONE DOOR.
//
// A generation is the provider request in flight, cancellable independently of
// the turn around it (steer.go). Four things cut one today — a person's steer, a
// person's model, a routed memory that landed before the first token, and a
// mark that says hand this over — and every one of them has to ask the same
// question first: is there a live attempt to cut at all, and may it be cut. The
// lock is what answers, and the answer is only single if there is one place that
// reads it.
//
// The measured cost of the alternative is in steer.go's own header: a second
// reader writing the same two lines somewhere else is how a build ends up with
// two answers to "is there anything to cut", and how a request that had just
// returned is cancelled after the fact.
//
// So the check is on the SOURCE. `a.generation.cancel(...)` may appear in
// exactly one function, [Agent.cutGenerationLocked]; every other road reaches it
// through that or through [Agent.cutGeneration] above it. A lane that adds a
// third cause adds a cause, never a second door.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// theCutDoor is the one function allowed to cancel a generation.
const theCutDoor = "cutGenerationLocked"

func TestEveryGenerationCutGoesThroughTheOneDoor(t *testing.T) {
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	cutters := map[string]string{}
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
		for _, decl := range file.Decls {
			function, isFunction := decl.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall || !cancelsAGeneration(call.Fun) {
					return true
				}
				where := fset.Position(call.Pos())
				cutters[function.Name.Name] = where.String()
				return true
			})
		}
	}
	if len(cutters) == 0 {
		t.Fatal("no generation cut found at all: either the field was renamed or the door was deleted — " +
			"this law is only worth anything while it can see the thing it guards")
	}
	for function, where := range cutters {
		if function == theCutDoor {
			continue
		}
		t.Errorf("%s cancels a generation in %s: every cut goes through %s, "+
			"which is the only reader of a.generation that can tell a live attempt "+
			"from one that returned a microsecond ago (steer.go). Add your cause "+
			"beside errSteerCut and call the door.", function, where, theCutDoor)
	}
}

// cancelsAGeneration recognises `<anything>.generation.cancel`, which is the one
// shape a cut has in this package.
func cancelsAGeneration(fun ast.Expr) bool {
	outer, isSelector := fun.(*ast.SelectorExpr)
	if !isSelector || outer.Sel.Name != "cancel" {
		return false
	}
	inner, isSelector := outer.X.(*ast.SelectorExpr)
	return isSelector && inner.Sel.Name == "generation"
}
