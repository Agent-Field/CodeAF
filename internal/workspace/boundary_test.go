package workspace

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Organization must remain usable without constructing an agent, opening a
// screen or enabling learned memory. The dependency boundary enforces that
// guarantee as later interfaces begin to consume the collection store.
func TestWorkspaceDoesNotDependOnExecutionOrOptionalMemory(t *testing.T) {
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
			for _, owner := range []string{"session", "tui", "tui2", "tui3", "store", "standing", "provider", "remote", "enginehost"} {
				forbidden := "github.com/Agent-Field/aforge-v2/internal/" + owner
				if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
					t.Errorf("%s depends on %s; resolve work in an adapter, not in organization storage", file.Name(), path)
				}
			}
		}
	}
}
