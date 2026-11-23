package serve

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Contract P2. A production caller must never accidentally evaluate the default policy.
func TestAccessConstructors(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		pos := token.NewFileSet()
		file, err := parser.ParseFile(pos, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typ, ok := literal.Type.(*ast.Ident)
			if !ok || !slices.Contains([]string{"Viewer", "wsViewer", "Access"}, typ.Name) {
				return true
			}
			for _, parent := range stack {
				switch parent := parent.(type) {
				case *ast.FuncDecl:
					if name == "item_access.go" && slices.Contains([]string{"itemViewer", "viewerOf", "access"}, parent.Name.Name) {
						return true
					}
				case *ast.ReturnStmt:
					if typ.Name != "Access" && len(literal.Elts) == 0 && len(parent.Results) > 1 {
						if last, ok := parent.Results[len(parent.Results)-1].(*ast.Ident); ok && (last.Name == "false" || last.Name == "err") {
							return true
						}
					}
				}
			}
			t.Errorf("%s builds %s outside the access constructors", pos.Position(node.Pos()), typ.Name)
			return true
		})
	}
}
