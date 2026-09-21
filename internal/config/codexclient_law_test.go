package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestC13NoSecondCodexProviderConstructorBypassesClientConfig(t *testing.T) {
	// C13: ClientConfigFor is the only door that may attach the Codex backend to provider.NewClient.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".codex-login-spec" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		set := token.NewFileSet()
		file, parseErr := parser.ParseFile(set, path, nil, 0)
		if parseErr != nil {
			return nil
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, _ := strconv.Unquote(literal.Value)
			if !strings.Contains(value, "chatgpt.com/backend-api/codex") {
				return true
			}
			// The vendored source and codexauth's constant describe the service;
			// neither constructs provider.Client. Any other literal is a second
			// base-url owner and therefore a bypass around ClientConfigFor.
			relative, _ := filepath.Rel(root, path)
			if relative != "internal/modelsource/modelsource.go" && relative != "internal/codexauth/tokens.go" {
				t.Errorf("%s names the Codex base URL outside its two declarative owners", set.Position(literal.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
