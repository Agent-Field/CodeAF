package workgraph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// maxComplexity is the repository's strict ceiling on a function's cyclomatic
// complexity, counted the way gocyclo counts it.
const maxComplexity = 15

// complexity is 1 plus every branch point in a function: if, for, range, a case
// or comm clause that is not the default, and each && or ||.
func complexity(fn *ast.FuncDecl) int {
	n := 1
	ast.Inspect(fn, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			n++
		case *ast.CaseClause:
			if x.List != nil {
				n++
			}
		case *ast.CommClause:
			if x.Comm != nil {
				n++
			}
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				n++
			}
		}
		return true
	})
	return n
}

// TestNoFunctionPassesTheComplexityCeiling holds this package to the ceiling
// without a waiver: a function that grows past it is split, not excused.
func TestNoFunctionPassesTheComplexityCeiling(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources: %v", err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				if c := complexity(fn); c > maxComplexity {
					t.Errorf("%s: %s has complexity %d, the ceiling is %d", name, fn.Name.Name, c, maxComplexity)
				}
			}
		}
	}
}
