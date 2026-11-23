package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EVERY ROLE IS RESOLVED THROUGH THE ONE READER (rolemodel.go). A file that read
// [Config.RolesSource] itself would be a resolution path the one-model promise
// does not reach, which is how the mark reader and the reflex left the one model.
// The only other use allowed is handing the whole source to a child's Config,
// where the child's own reader answers it (OneModel travels beside it).
func TestEveryRoleResolutionGoesThroughTheRoleReader(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "rolemodel.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		copied := map[ast.Node]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if kv, ok := n.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "RolesSource" {
					copied[kv.Value] = true
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "RolesSource" && !copied[sel] {
				t.Errorf("%s reads RolesSource directly; resolve through the role reader (rolemodel.go)", fset.Position(sel.Pos()))
			}
			return true
		})
	}
}
