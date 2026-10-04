package cellstore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// L2: only the harness commits. A Turn is a sealed commit, so exactly one
// function builds one: newTurn in turn.go. This walks every non-test file in
// the tree and refuses a Turn composite literal anywhere else.
func TestL2OnlyNewTurnConstructsATurn(t *testing.T) {
	var offenders []string
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || (d.IsDir() && skipDir(d.Name())) || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		offenders = append(offenders, turnLiterals(t, path)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 0 {
		t.Fatalf("Turn built outside newTurn (law L2): %v", offenders)
	}
}

func skipDir(name string) bool {
	return name == ".git" || name == "node_modules" || name == "target" || name == "third_party"
}

func turnLiterals(t *testing.T, path string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil
	}
	inCellstore := f.Name.Name == "cellstore"
	var out []string
	for _, decl := range f.Decls {
		fn, isFn := decl.(*ast.FuncDecl)
		allowed := isFn && fn.Name.Name == "newTurn" && inCellstore
		ast.Inspect(decl, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok && !allowed && isTurnType(lit.Type, inCellstore) {
				out = append(out, fset.Position(lit.Pos()).String())
			}
			return true
		})
	}
	return out
}

// isTurnType matches Turn inside this package and cellstore.Turn outside it.
func isTurnType(e ast.Expr, inside bool) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return inside && x.Name == "Turn"
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		return ok && pkg.Name == "cellstore" && x.Sel.Name == "Turn"
	}
	return false
}
