package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNothingUnderTheBackendLockCallsIntoAConversation is appSessionLocks
// (app.go's locked): a method called on a conversation (`….Conv.Title()`,
// `appContextOf(….Conv)`) inside a b.locked closure waits on that
// conversation's own lock while holding the whole app's.
func TestNothingUnderTheBackendLockCallsIntoAConversation(t *testing.T) {
	files, err := filepath.Glob("app*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isBackendLocked(call) || len(call.Args) != 1 {
				return true
			}
			body, ok := call.Args[0].(*ast.FuncLit)
			if !ok {
				return true
			}
			ast.Inspect(body.Body, func(inner ast.Node) bool {
				used, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				if calledOnConv(used.Fun) || (isIdent(used.Fun, "appContextOf") && len(used.Args) == 1 && isConv(used.Args[0])) {
					t.Errorf("%s: a conversation is called while b.mu is held; read it before b.locked (appSessionLocks)", fset.Position(used.Pos()))
				}
				return true
			})
			return true
		})
	}
}

func isBackendLocked(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "locked" && isIdent(selector.X, "b")
}

// calledOnConv is `X.Conv.Method` as a call's function.
func calledOnConv(fun ast.Expr) bool {
	selector, ok := fun.(*ast.SelectorExpr)
	return ok && isConv(selector.X)
}

func isConv(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Conv"
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}
