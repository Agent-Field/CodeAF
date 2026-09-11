package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLoadReadsOneProfileSnapshotForTheKeyAndServices keeps startup from
// independently reading config.json for APIKey and model_sources. Both facts
// must be derived from the one snapshot read at the launch door.
func TestLoadReadsOneProfileSnapshotForTheKeyAndServices(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate config.go")
	}
	path := filepath.Join(filepath.Dir(here), "config.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var load *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "load" {
			load = function
			break
		}
	}
	if load == nil {
		t.Fatal("config.load is missing")
	}

	calls := make(map[string]int)
	ast.Inspect(load.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, ok := call.Fun.(*ast.Ident)
		if ok {
			calls[name.Name]++
		}
		return true
	})
	if calls["readProfileConfig"] != 1 {
		t.Fatalf("config.load calls readProfileConfig %d times; want exactly one shared snapshot", calls["readProfileConfig"])
	}
	for _, oldDoor := range []string{"PersistedAPIKey", "PersistedSources", "ResolveSources"} {
		if calls[oldDoor] != 0 {
			t.Errorf("config.load calls %s; derive the key and services from its shared profile snapshot", oldDoor)
		}
	}
}
