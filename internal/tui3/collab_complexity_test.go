package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestCoordinateFunctionsStayUnderTheCeiling(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !strings.HasPrefix(name, "collab") {
			continue
		}
		source, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			got := collabComplexity(function.Body)
			if got > 15 {
				t.Errorf("%s %s is %d, ceiling is 15", name, function.Name.Name, got)
			}
		}
	}
}

func collabComplexity(body *ast.BlockStmt) int {
	decisions := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			decisions++
		case *ast.CaseClause:
			if typed.List != nil {
				decisions++
			}
		case *ast.BinaryExpr:
			if typed.Op.String() == "&&" || typed.Op.String() == "||" {
				decisions++
			}
		}
		return true
	})
	return decisions
}
