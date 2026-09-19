package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestCollabFilesDoNotImportTheRouterPackage(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		if file.Name() != "collab.go" && file.Name() != "tools_coordinate.go" && file.Name() != "mailbox.go" && file.Name() != "collab_consult.go" {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range source.Imports {
			path := spec.Path.Value
			if strings.Contains(path, "/internal/wscollab") {
				t.Errorf("%s imports wscollab; the host binds the router", file.Name())
			}
		}
	}
}

func TestNewCollabFunctionsStayUnderTheCeiling(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		if file.Name() != "collab.go" && file.Name() != "tools_coordinate.go" && file.Name() != "collab_consult.go" {
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
