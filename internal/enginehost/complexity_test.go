package enginehost

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

const theCeiling = 15

func TestNewFunctionsStayUnderTheCeiling(t *testing.T) {
	// THE CEILING APPLIES TO THE WAVE 3 DOOR. The rest of this package predates
	// it, and rewriting Host.open to satisfy a new count is not this lane.
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, file := range files {
		if file.Name() != "collab.go" && file.Name() != "collabbind.go" {
			continue
		}
		source, err := parser.ParseFile(set, file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			got := cyclomatic(function.Body)
			if got > theCeiling {
				t.Errorf("%s %s is %d, ceiling is %d", file.Name(), function.Name.Name, got, theCeiling)
			}
		}
	}
}

func cyclomatic(body *ast.BlockStmt) int {
	decisions := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			decisions++
		case *ast.CaseClause:
			if typed.List != nil {
				decisions++
			}
		case *ast.CommClause:
			if typed.Comm != nil {
				decisions++
			}
		case *ast.BinaryExpr:
			if typed.Op == token.LAND || typed.Op == token.LOR {
				decisions++
			}
		}
		return true
	})
	return decisions
}
