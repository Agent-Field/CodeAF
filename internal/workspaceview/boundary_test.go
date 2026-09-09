package workspaceview

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The engine consumes the injected projection without importing its adapter.
// Keeping that direction makes organization usable without constructing a UI.
func TestSessionDoesNotImportItsOrganizationAdapter(t *testing.T) {
	files, err := filepath.Glob("../session/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range source.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if imp == "github.com/Agent-Field/aforge-v2/internal/workspaceview" {
				t.Errorf("%s imports its adapter", path)
			}
		}
	}
}
