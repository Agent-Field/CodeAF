package config

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestOnlyConfigTurnsAnAccountIntoProviderSettings states the construction
// law: API keys and base URLs enter provider.Config only through this package,
// so every caller inherits the model-level split and every future account
// setting instead of preserving a stale private copy of the door.
func TestOnlyConfigTurnsAnAccountIntoProviderSettings(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the client-door law")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	set := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(relative, filepath.Join("internal", "config")+string(filepath.Separator)) {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(set, path, source, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			selector, ok := literal.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, packageOK := selector.X.(*ast.Ident)
			if !packageOK || pkg.Name != "provider" || selector.Sel.Name != "Config" {
				return true
			}
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				name, nameOK := field.Key.(*ast.Ident)
				if !ok || !nameOK || (name.Name != "APIKey" && name.Name != "BaseURL") {
					continue
				}
				position := set.Position(name.Pos())
				t.Errorf("%s:%d sets provider.Config.%s outside internal/config; use config.ClientConfig or config.ClientConfigFor instead", filepath.ToSlash(relative), position.Line, name.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(fmt.Errorf("walk repository for provider client construction: %w", err))
	}
}
