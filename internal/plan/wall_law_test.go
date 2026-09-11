package plan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestPlanningReachesAModelOnlyThroughTheClientItIsHanded is the planning side
// of issue #927's law: no planning call is built without the budget its wall
// implies.
//
// THE BUDGET IS ATTACHED BY THE WALL, SO A PLANNING CALL MUST GO THROUGH ONE.
// The structuring slot the resident and `aforge do` hand this package is walled
// (internal/provider/pool), and the wall is what tells the model how long it
// may think and keeps what it thought if it runs out. That guarantee holds for
// every pass here only while no pass can reach a model any other way — so this
// package, and the intent compiler beside it, may not build a client of their
// own: no adapter constructed, and no package imported that constructs one.
// Every model call they make is on the Completer they were given.
func TestPlanningReachesAModelOnlyThroughTheClientItIsHanded(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join("..", "head", "compiler.go"))
	// Each of these hands out a model client: the configuration's own
	// constructors, the pooled slots, and the router over them.
	builders := map[string]bool{
		"github.com/Agent-Field/aforge-v2/internal/config":        true,
		"github.com/Agent-Field/aforge-v2/internal/provider/pool": true,
		"github.com/Agent-Field/aforge-v2/internal/router":        true,
	}
	fileset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileset, name, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if builders[path] {
				t.Errorf("%s imports %s, which can build a model client that no wall tells how long it has", name, path)
			}
		}
		whole, err := parser.ParseFile(fileset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(whole, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "provider" && selector.Sel.Name == "NewClient" {
				t.Errorf("%s: %s builds a model adapter of its own; a planning call goes through the client it was handed",
					name, fileset.Position(call.Pos()))
			}
			return true
		})
	}
}
