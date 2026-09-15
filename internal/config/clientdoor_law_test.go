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
	"strconv"
	"strings"
	"testing"
)

const (
	providerImport    = "github.com/Agent-Field/codeaf/internal/provider"
	modelsourceImport = "github.com/Agent-Field/codeaf/internal/modelsource"
)

// TestOnlyConfigTurnsAnAccountIntoProviderSettings states the construction
// law: API keys and base URLs enter provider.Config only through this package.
// Aliases, positional literals, and assignments after construction are all
// construction; spelling one differently must not turn it into an escape.
func TestOnlyConfigTurnsAnAccountIntoProviderSettings(t *testing.T) {
	walkDoorSources(t, func(relative string, set *token.FileSet, file *ast.File) {
		aliases := importAliases(file, providerImport, "provider")
		if len(aliases) == 0 {
			return
		}
		inspectGuardedType(t, relative, set, file, aliases, "Config", map[string]bool{
			"APIKey": true, "BaseURL": true,
		}, "provider.Config", "use config.ClientConfig or config.ClientConfigFor instead")
	})
}

// TestNobodyHandsTheClientDoorAKeyOrABase keeps source resolution in config.
// A caller may carry a Set, but may not construct or mutate Connected.Key or
// Connected.Address beside the one authoritative door.
func TestNobodyHandsTheClientDoorAKeyOrABase(t *testing.T) {
	walkDoorSources(t, func(relative string, set *token.FileSet, file *ast.File) {
		aliases := importAliases(file, modelsourceImport, "modelsource")
		if len(aliases) == 0 {
			return
		}
		inspectGuardedType(t, relative, set, file, aliases, "Connected", map[string]bool{
			"Key": true, "Address": true,
		}, "modelsource.Connected", "carry a modelsource.Set resolved by internal/config instead")
	})
}

func walkDoorSources(t *testing.T, inspect func(string, *token.FileSet, *ast.File)) {
	t.Helper()
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
		inspect(filepath.ToSlash(relative), set, file)
		return nil
	})
	if err != nil {
		t.Fatal(fmt.Errorf("walk repository for client-door construction: %w", err))
	}
}

func importAliases(file *ast.File, path, ordinary string) map[string]bool {
	aliases := make(map[string]bool)
	for _, spec := range file.Imports {
		unquoted, err := strconv.Unquote(spec.Path.Value)
		if err != nil || unquoted != path {
			continue
		}
		name := ordinary
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name != "." && name != "_" {
			aliases[name] = true
		}
	}
	return aliases
}

func guardedSelector(expr ast.Expr, aliases map[string]bool, typeName string) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != typeName {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && aliases[pkg.Name]
}

func inspectGuardedType(t *testing.T, relative string, set *token.FileSet, file *ast.File, aliases map[string]bool, typeName string, fields map[string]bool, display, remedy string) {
	t.Helper()
	typed := make(map[string]bool)
	mark := func(name *ast.Ident, expr ast.Expr) {
		if name != nil && guardedSelector(expr, aliases, typeName) {
			typed[name.Name] = true
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch item := node.(type) {
		case *ast.Field:
			for _, name := range item.Names {
				mark(name, item.Type)
			}
		case *ast.ValueSpec:
			for _, name := range item.Names {
				mark(name, item.Type)
			}
			for index, value := range item.Values {
				if index < len(item.Names) && isGuardedLiteral(value, aliases, typeName) {
					typed[item.Names[index].Name] = true
				}
			}
		case *ast.AssignStmt:
			for index, value := range item.Rhs {
				if index >= len(item.Lhs) || !isGuardedLiteral(value, aliases, typeName) {
					continue
				}
				if name, ok := item.Lhs[index].(*ast.Ident); ok {
					typed[name.Name] = true
				}
			}
		}
		return true
	})

	ast.Inspect(file, func(node ast.Node) bool {
		switch item := node.(type) {
		case *ast.CompositeLit:
			if !guardedSelector(item.Type, aliases, typeName) {
				return true
			}
			for _, element := range item.Elts {
				field, keyed := element.(*ast.KeyValueExpr)
				if !keyed {
					position := set.Position(element.Pos())
					t.Errorf("%s:%d constructs %s positionally outside internal/config; %s", relative, position.Line, display, remedy)
					continue
				}
				name, ok := field.Key.(*ast.Ident)
				if ok && fields[name.Name] {
					position := set.Position(name.Pos())
					t.Errorf("%s:%d sets %s.%s outside internal/config; %s", relative, position.Line, display, name.Name, remedy)
				}
			}
		case *ast.AssignStmt:
			for _, left := range item.Lhs {
				selector, ok := left.(*ast.SelectorExpr)
				if !ok || !fields[selector.Sel.Name] {
					continue
				}
				name, ok := selector.X.(*ast.Ident)
				if !ok || !typed[name.Name] {
					continue
				}
				position := set.Position(selector.Sel.Pos())
				t.Errorf("%s:%d assigns %s.%s outside internal/config; %s", relative, position.Line, display, selector.Sel.Name, remedy)
			}
		}
		return true
	})
}

func isGuardedLiteral(expr ast.Expr, aliases map[string]bool, typeName string) bool {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	literal, ok := expr.(*ast.CompositeLit)
	return ok && guardedSelector(literal.Type, aliases, typeName)
}
