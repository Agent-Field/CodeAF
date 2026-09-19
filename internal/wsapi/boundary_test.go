package wsapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestNoLookalikeWorkspaceTypes(t *testing.T) {
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
				switch named.Name.Name {
				case "Provenance", "MembershipEvent":
					t.Errorf("%s declares %s; use workspace.%s", file.Name(), named.Name.Name, named.Name.Name)
				}
			}
		}
	}
}

func TestServiceDoesNotImportSessionTUIProviderOrRun(t *testing.T) {
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
			for _, owner := range []string{"session", "tui3", "provider", "run", "wsdiscover", "wscollab"} {
				forbidden := "github.com/Agent-Field/codeaf/internal/" + owner
				if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
					t.Errorf("%s imports %s; wsapi talks to workspace only", file.Name(), path)
				}
			}
		}
	}
}

func TestNoStoreAdapterFallback(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") {
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
				if named.Name.Name == "storeAdapter" {
					t.Errorf("%s still declares storeAdapter; Open must use *workspace.Store", file.Name())
				}
			}
		}
	}
}

func TestProductionFilesDoNotWriteSQL(t *testing.T) {
	needles := []string{"INSERT INTO", "SELECT ", "CREATE TABLE", "DELETE FROM", "PRAGMA "}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatal(err)
		}
		body := string(source)
		for _, needle := range needles {
			if strings.Contains(body, needle) {
				t.Errorf("%s writes SQL (%q); the model never writes SQL and neither does wsapi", file.Name(), needle)
			}
		}
	}
}
