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

func TestC12EverySourceScopedCatalogUsesTheConnectedServiceConstructor(t *testing.T) {
	// C12 and C18: Source is the mark of a connected service's private cache
	// compartment. Only CatalogOptionsFor may assemble one, so adding a catalog
	// call cannot omit the account-aware Codex client while still compiling.
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
			return parseErr
		}
		catalogNames := make(map[string]bool)
		for _, imported := range file.Imports {
			value, _ := strconv.Unquote(imported.Path.Value)
			if value != "github.com/Agent-Field/codeaf/internal/catalog" {
				continue
			}
			name := "catalog"
			if imported.Name != nil {
				name = imported.Name.Name
			}
			catalogNames[name] = true
		}
		if len(catalogNames) == 0 {
			return nil
		}
		var constructor *ast.FuncDecl
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Name.Name == "CatalogOptionsFor" {
				constructor = function
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			selector, ok := literal.Type.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Options" {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok || !catalogNames[owner.Name] {
				return true
			}
			hasSource := false
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, named := field.Key.(*ast.Ident)
				if named && name.Name == "Source" {
					hasSource = true
					break
				}
			}
			if !hasSource {
				return true
			}
			insideConstructor := constructor != nil && literal.Pos() >= constructor.Pos() && literal.End() <= constructor.End()
			if !insideConstructor {
				t.Errorf("%s sets catalog.Options.Source outside config.CatalogOptionsFor", set.Position(literal.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
