package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"os/exec"
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
	violations, err := catalogSourceViolations(root)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

const catalogImportPath = "github.com/Agent-Field/codeaf/internal/catalog"

func catalogSourceViolations(root string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
			if value != catalogImportPath {
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
				violations = append(violations, fmtCatalogSourceViolation(set.Position(literal.Pos())))
			}
			return true
		})

		// A literal is not the only way to write the field. Type information is
		// what distinguishes options.Source from every unrelated Source field,
		// including when options was copied from a field or helper first.
		info := &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}
		checker := types.Config{
			Importer: newCatalogLawImporter(),
			Error:    func(error) {},
		}
		_, _ = checker.Check(file.Name.Name, set, []*ast.File{file}, info)
		ast.Inspect(file, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, expression := range assignment.Lhs {
				selector, ok := expression.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Source" || !isCatalogOptions(info.TypeOf(selector.X)) {
					continue
				}
				insideConstructor := constructor != nil && selector.Pos() >= constructor.Pos() && selector.End() <= constructor.End()
				if !insideConstructor {
					violations = append(violations, fmtCatalogSourceViolation(set.Position(selector.Pos())))
				}
			}
			return true
		})
		return nil
	})
	return violations, err
}

func fmtCatalogSourceViolation(position token.Position) string {
	return position.String() + " sets catalog.Options.Source outside config.CatalogOptionsFor"
}

func isCatalogOptions(value types.Type) bool {
	if value == nil {
		return false
	}
	value = types.Unalias(value)
	if pointer, ok := value.(*types.Pointer); ok {
		value = types.Unalias(pointer.Elem())
	}
	named, ok := value.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Options" && named.Obj().Pkg().Path() == catalogImportPath
}

type catalogLawImporter struct {
	packages map[string]*types.Package
}

func newCatalogLawImporter() *catalogLawImporter {
	return &catalogLawImporter{packages: make(map[string]*types.Package)}
}

func (i *catalogLawImporter) Import(path string) (*types.Package, error) {
	if imported := i.packages[path]; imported != nil {
		return imported, nil
	}
	name := path
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	imported := types.NewPackage(path, name)
	i.packages[path] = imported
	if path == catalogImportPath {
		field := types.NewField(token.NoPos, imported, "Source", types.Typ[types.String], false)
		structure := types.NewStruct([]*types.Var{field}, nil)
		object := types.NewTypeName(token.NoPos, imported, "Options", nil)
		types.NewNamed(object, structure, nil)
		imported.Scope().Insert(object)
	}
	imported.MarkComplete()
	return imported, nil
}

func TestC12CatalogSourceAssignmentFixtureIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "catalog"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":                      "module github.com/Agent-Field/codeaf\n\ngo 1.26.5\n",
		"internal/catalog/catalog.go": "package catalog\n\ntype Options struct { Source string }\n",
		"fixture/fixture.go": `package fixture

import "github.com/Agent-Field/codeaf/internal/catalog"

type shelf struct { options catalog.Options }

func (s shelf) bypass() {
	options := s.options
	options.Source = "codex"
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("the catalog assignment fixture does not compile: %v\n%s", err, output)
	}
	violations, err := catalogSourceViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || !strings.Contains(violations[0], "fixture.go") {
		t.Fatalf("catalog assignment fixture violations = %v, want its Source assignment", violations)
	}
}
