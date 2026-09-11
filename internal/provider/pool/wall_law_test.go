package pool

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

// TestEveryCompletionThroughAWallIsToldItsWall is the wall's law stated over
// this package's own source (issue #927).
//
// A WALLED COMPLETION IS SENT FROM ONE PLACE, AND THAT PLACE TELLS THE MODEL ITS
// WALL. The structuring slots reach the model through [walled], and a second
// line in it that called the wrapped client directly would be a completion under
// a stopwatch the model was never shown — which is exactly the call issue #927
// lost four minutes of thought to. So the wrapped client is called from
// [walled.completion] and from nowhere else, and that function carries the wall
// to the effort ladder before it sends.
func TestEveryCompletionThroughAWallIsToldItsWall(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fileset := token.NewFileSet()
	senders := map[string]bool{}
	tells := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fileset, name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			enclosing := functionName(function)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if receiver, ok := selector.X.(*ast.SelectorExpr); ok &&
					receiver.Sel.Name == "inner" && selector.Sel.Name == "CompleteWithMessages" {
					senders[enclosing] = true
				}
				if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "provider" && selector.Sel.Name == "WithThinkingWall" {
					tells[enclosing] = true
				}
				return true
			})
		}
	}
	names := make([]string, 0, len(senders))
	for name := range senders {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != 1 || names[0] != "walled.completion" {
		t.Fatalf("the wrapped client is called from %v; a walled completion is sent from walled.completion and nowhere else, "+
			"because that is the one place the model is told its wall", names)
	}
	if !tells["walled.completion"] {
		t.Fatal("walled.completion no longer carries its wall to the effort ladder (provider.WithThinkingWall); " +
			"every completion under a wall would be cut by a stopwatch its model was never shown")
	}
}

// functionName is a declaration's name as a reader would say it: the receiver's
// type and the method, or the function alone.
func functionName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	if star, ok := receiver.(*ast.StarExpr); ok {
		receiver = star.X
	}
	if ident, ok := receiver.(*ast.Ident); ok {
		return ident.Name + "." + function.Name.Name
	}
	return function.Name.Name
}
