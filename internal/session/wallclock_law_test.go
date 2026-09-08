package session

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"testing"
)

// TestEveryBudgetEndingIsSpent holds the structural half of the wall's
// contract: every source reader of Budget.Exhausted reaches the one constructor
// for a budget ending, and no other Decision constructor can quietly acquire
// the privilege of ending over moving work.
func TestEveryBudgetEndingIsSpent(t *testing.T) {
	exhaustionCalls := 0
	spentConstructor := false
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			owner := functionName(function)
			readsExhaustion, callsStopSpent := false, false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CallExpr:
					if selector, ok := typed.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Exhausted" {
						readsExhaustion = true
						exhaustionCalls++
					}
					if name, ok := typed.Fun.(*ast.Ident); ok && name.Name == "stopSpent" {
						callsStopSpent = true
					}
				case *ast.CompositeLit:
					name, ok := typed.Type.(*ast.Ident)
					if !ok || name.Name != "Decision" {
						return true
					}
					for _, element := range typed.Elts {
						field, ok := element.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := field.Key.(*ast.Ident)
						if !ok || key.Name != "Spent" {
							continue
						}
						value, trueValue := field.Value.(*ast.Ident)
						if owner != "stopSpent" || !trueValue || value.Name != "true" {
							t.Errorf("%s:%d: %s sets Decision.Spent outside stopSpent", filepath.Base(path), fset.Position(field.Pos()).Line, owner)
						}
						if owner == "stopSpent" && trueValue && value.Name == "true" {
							spentConstructor = true
						}
					}
				case *ast.AssignStmt:
					for _, target := range typed.Lhs {
						selector, ok := target.(*ast.SelectorExpr)
						if ok && selector.Sel.Name == "Spent" {
							t.Errorf("%s:%d: %s assigns Decision.Spent outside its constructor", filepath.Base(path), fset.Position(selector.Pos()).Line, owner)
						}
					}
				}
				return true
			})
			if readsExhaustion && !callsStopSpent {
				t.Errorf("%s: %s reads Budget.Exhausted without answering through stopSpent", filepath.Base(path), owner)
			}
		}
	})
	if exhaustionCalls == 0 {
		t.Fatal("no source reads Budget.Exhausted; the law is watching nothing")
	}
	if !spentConstructor {
		t.Fatal("stopSpent no longer constructs a Decision with Spent set")
	}
}
