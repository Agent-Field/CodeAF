package anchors

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoFixedSubjectList rejects literal dictionaries in the production
// package. The only vocabulary exception is the filesystem-container switch;
// confirmed names and proper-token candidates must come from the caller.
func TestNoFixedSubjectList(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if ok && fn.Name.Name == "genericRoot" {
				continue
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.CompositeLit:
					if len(n.Elts) == 0 {
						break
					}
					var key ast.Expr
					switch kind := n.Type.(type) {
					case *ast.ArrayType:
						key = kind.Elt
					case *ast.MapType:
						key = kind.Key
					}
					if id, ok := key.(*ast.Ident); ok && id.Name == "string" {
						t.Errorf("%s: literal vocabulary; derive subjects from the corpus", fset.Position(n.Pos()))
					}
				case *ast.CaseClause:
					for _, expression := range n.List {
						if literal, ok := expression.(*ast.BasicLit); ok && literal.Kind == token.STRING {
							t.Errorf("%s: subject switch outside the filesystem-container rule", fset.Position(n.Pos()))
						}
					}
				}
				return true
			})
		}
	}
}
