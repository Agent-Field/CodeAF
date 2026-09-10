package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A MODEL OVERRIDE MAY APPEAR ONLY BESIDE THE CLIENT-ACCOUNT DOOR. WithModel
// changes a request slug without changing its address or bearer, so using it
// anywhere else can send a role's model to the conversation's service.
func TestOnlyTheClientDoorCanOverrideAModel(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	doors := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		aliases := make(map[string]bool)
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil || path != "github.com/Agent-Field/agentfield/sdk/go/ai" {
				continue
			}
			alias := "ai"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			aliases[alias] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, named := selector.X.(*ast.Ident)
			if !named || selector.Sel.Name != "WithModel" || !aliases[pkg.Name] {
				return true
			}
			if name == "clientdoor.go" {
				doors++
			} else {
				t.Errorf("%s:%d calls ai.WithModel outside clientdoor.go", name, files.Position(call.Pos()).Line)
			}
			return true
		})
	}
	if doors != 1 {
		t.Errorf("clientdoor.go contains %d ai.WithModel calls, want the one model-override door", doors)
	}
}
