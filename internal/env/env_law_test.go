package env

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

// H3: every product read of an owned environment name that syntax can identify
// goes through this package, including calls whose argument is an owned package
// constant. A genuinely dynamic name is outside a static law's reach;
// env.Value is the required door for that shape.
func TestH3OwnedEnvironmentReadsUseTheOneDoor(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := ownedReadViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func ownedReadViolations(root string) ([]string, error) {
	ownedConstants, err := packageConstantsWithOwnedValues(root)
	if err != nil {
		return nil, err
	}
	var violations []string
	err = walkGo(root, func(path string, file *ast.File, set *token.FileSet) {
		relative := repoRelative(root, path)
		if strings.HasPrefix(relative, "internal/env/") || strings.HasSuffix(path, "_test.go") {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if ranged, ok := node.(*ast.RangeStmt); ok && isOSEnviron(ranged.X) {
				ast.Inspect(ranged.Body, func(inside ast.Node) bool {
					switch value := inside.(type) {
					case *ast.BasicLit:
						if ownedArgument(value, ownedConstants) {
							position := set.Position(value.Pos())
							violations = append(violations, fmt.Sprintf("%s:%d reads an owned name out of os.Environ; put the read in internal/env", relative, position.Line))
						}
					case *ast.Ident:
						if ownedConstants[value.Name] {
							position := set.Position(value.Pos())
							violations = append(violations, fmt.Sprintf("%s:%d reads an owned constant out of os.Environ; put the read in internal/env", relative, position.Line))
						}
					case *ast.SelectorExpr:
						if ownedConstants[value.Sel.Name] {
							position := set.Position(value.Pos())
							violations = append(violations, fmt.Sprintf("%s:%d reads an owned constant out of os.Environ; put the read in internal/env", relative, position.Line))
						}
					}
					return true
				})
			}
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 || !isOSRead(call.Fun) {
				return true
			}
			if ownedArgument(call.Args[0], ownedConstants) {
				position := set.Position(call.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d reads an owned environment variable through os; use internal/env", relative, position.Line))
			}
			return true
		})
	})
	return violations, err
}

func TestEnvironmentLawRejectsEveryStaticallyVisibleOwnedReadShape(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"owned/names.go":   "package owned\nconst Home = \"CODEAF_HOME\"\n",
		"literal/live.go":  "package literal\nimport \"os\"\nvar Value = os.Getenv(\"CODEAF_HOME\")\n",
		"constant/live.go": "package constant\nimport \"os\"\nconst Home = \"CODEAF_HOME\"\nvar Value = os.LookupEnv(Home)\n",
		"selector/live.go": "package selector\nimport (\"os\"; \"example/owned\")\nvar Value = os.Getenv(owned.Home)\n",
		"environ/live.go":  "package environ\nimport (\"os\"; \"strings\")\nfunc Read() { for _, entry := range os.Environ() { if strings.HasPrefix(entry, \"CODEAF_\") {} } }\n",
	}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ownedReadViolations(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"literal/live.go", "constant/live.go", "selector/live.go", "environ/live.go"} {
		found := false
		for _, violation := range got {
			found = found || strings.Contains(violation, path+":")
		}
		if !found {
			t.Errorf("law accepted %s: %v", path, got)
		}
	}
}

// H3: table-driven families use the dynamic door, so a CODEAF_ value held in
// a row gets the same fallback without making foreign provider variables ours.
func TestH3DynamicEnvironmentFamiliesUseTheDynamicDoor(t *testing.T) {
	minimum := map[string]int{
		"cmd/codeaf/chatv3.go":             1,
		"cmd/codeaf/do.go":                 1,
		"cmd/codeaf/exec.go":               1,
		"cmd/codeaf/run.go":                1,
		"internal/config/config.go":        1,
		"internal/config/projectconfig.go": 1,
		"internal/config/seats.go":         1,
		"internal/config/settings.go":      5,
		"internal/config/sources.go":       2,
		"internal/ctxbudget/ctxbudget.go":  1,
		"internal/tui3/app.go":             1,
		"internal/tui3/onboarding.go":      1,
	}
	root := repositoryRoot(t)
	for relative, want := range minimum {
		path := filepath.Join(root, filepath.FromSlash(relative))
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		got := 0
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "Value" || selector.Sel.Name == "LookupValue" {
				got++
			}
			return true
		})
		if got < want {
			t.Errorf("%s has %d dynamic environment door(s), want at least %d", relative, got, want)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate environment law")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}

func packageConstantsWithOwnedValues(root string) (map[string]bool, error) {
	names := make(map[string]bool)
	err := walkGo(root, func(_ string, file *ast.File, _ *token.FileSet) {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, raw := range general.Specs {
				spec := raw.(*ast.ValueSpec)
				for index, value := range spec.Values {
					literal, ok := value.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING || index >= len(spec.Names) {
						continue
					}
					text, err := strconv.Unquote(literal.Value)
					if err == nil && ownedName(text) {
						names[spec.Names[index].Name] = true
					}
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

func walkGo(root string, visit func(string, *ast.File, *token.FileSet)) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "third_party", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", repoRelative(root, path), err)
		}
		visit(path, file, set)
		return nil
	})
}

func isOSRead(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || (selector.Sel.Name != "Getenv" && selector.Sel.Name != "LookupEnv") {
		return false
	}
	name, ok := selector.X.(*ast.Ident)
	return ok && name.Name == "os"
}

func isOSEnviron(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Environ" {
		return false
	}
	name, ok := selector.X.(*ast.Ident)
	return ok && name.Name == "os"
}

func ownedArgument(expr ast.Expr, constants map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return false
		}
		text, err := strconv.Unquote(value.Value)
		return err == nil && ownedName(text)
	case *ast.Ident:
		return constants[value.Name]
	case *ast.SelectorExpr:
		return constants[value.Sel.Name]
	default:
		return false
	}
}

func ownedName(name string) bool {
	return strings.HasPrefix(name, prefix) || strings.HasPrefix(name, legacyPrefix)
}

func repoRelative(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}
