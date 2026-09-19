package wscollab

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

const theCeiling = 15

func TestNewFunctionsStayUnderTheCeiling(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := parser.ParseFile(set, file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			got := cyclomatic(function.Body)
			if got > theCeiling {
				t.Errorf("%s %s is %d, ceiling is %d", file.Name(), function.Name.Name, got, theCeiling)
			}
		}
	}
}

func cyclomatic(body *ast.BlockStmt) int {
	decisions := 1
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			decisions++
		case *ast.CaseClause:
			if typed.List != nil {
				decisions++
			}
		case *ast.CommClause:
			if typed.Comm != nil {
				decisions++
			}
		case *ast.BinaryExpr:
			if typed.Op == token.LAND || typed.Op == token.LOR {
				decisions++
			}
		}
		return true
	})
	return decisions
}

func TestPackageDoesNotImportSessionOrWorkspace(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range source.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, owner := range []string{"session", "tui3", "wsapi", "workspace", "enginehost", "cmd"} {
				forbidden := "github.com/Agent-Field/codeaf/internal/" + owner
				if owner == "cmd" {
					forbidden = "github.com/Agent-Field/codeaf/cmd"
				}
				if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
					t.Errorf("%s imports %s; the host binds this package", file.Name(), path)
				}
			}
		}
	}
}

func TestPlannerAndCriticAreNotTypes(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				named, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				switch strings.ToLower(named.Name.Name) {
				case "planner", "critic", "manager":
					t.Errorf("%s declares %s; roles are configurable strings", file.Name(), named.Name.Name)
				}
			}
		}
	}
}
