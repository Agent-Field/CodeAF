package remote

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoWireMethodCanWriteThePermitBook enforces the local-only boundary on the
// permit book: no wire handler or non-test file in internal/remote may import
// internal/pair or call PermitBook.Set, Permits, PermitsAt or write
// permits.json. Only this computer's own operator doors may change who can
// watch or type here.
func TestNoWireMethodCanWriteThePermitBook(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Clean(name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		if strings.Contains(string(raw), "permits.json") {
			t.Errorf("%s references permits.json", path)
		}
		file, err := parser.ParseFile(fset, path, raw, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", path, err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/Agent-Field/codeaf/internal/pair" {
				t.Errorf("%s imports internal/pair; wire methods must never reach the permit book", path)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Permits", "PermitsAt", "PermitBook":
				t.Errorf("%s:%d references %s", path, fset.Position(sel.Pos()).Line, sel.Sel.Name)
			}
			return true
		})
	}
}
