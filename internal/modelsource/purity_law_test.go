package modelsource

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultServiceIdentityIsSpelledOnce(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	set := token.NewFileSet()
	var definitions []token.Position
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "third_party", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// LaneOpenRouter names a routing mode, not a model-service identity.
		// Its deliberately identical wire word is owned by the lane registry.
		if filepath.Clean(path) == filepath.Join(root, "internal", "config", "settings.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && value == "openrouter" {
				definitions = append(definitions, set.Position(literal.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || filepath.Clean(definitions[0].Filename) != filepath.Join(filepath.Dir(here), "modelsource.go") {
		t.Fatalf("default service identity must be defined once in modelsource.DefaultID; found %v", definitions)
	}
}

func TestModelsourceReadsNothingAndDialsNothing(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate modelsource")
	}
	dir := filepath.Dir(here)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"os": true, "net": true, "net/http": true, "io/ioutil": true,
		"github.com/Agent-Field/aforge-v2/internal/config":   true,
		"github.com/Agent-Field/aforge-v2/internal/catalog":  true,
		"github.com/Agent-Field/aforge-v2/internal/provider": true,
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if forbidden[path] {
				t.Errorf("%s imports forbidden package %s", entry.Name(), path)
			}
		}
	}
}
