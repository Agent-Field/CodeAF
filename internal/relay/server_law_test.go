package relay

import (
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// THE STRUCTURAL HALF OF "THE RELAY CANNOT READ A FRAME". A promise in a
// comment is worth nothing; an import that does not exist is worth something.
// This package must not be able to reach the session wire or the crypto that
// rides it, because a package that cannot name a type cannot decode one.
func TestTheRelayCannotEvenNameTheThingsItCarries(t *testing.T) {
	forbidden := []string{
		"github.com/Agent-Field/codeaf/internal/remote",
		"github.com/Agent-Field/codeaf/internal/pair",
		"github.com/Agent-Field/codeaf/internal/session",
	}
	set := token.NewFileSet()
	packages, err := parser.ParseDir(set, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		for path, file := range pkg.Files {
			for _, imported := range file.Imports {
				for _, banned := range forbidden {
					if strings.Trim(imported.Path.Value, `"`) == banned {
						t.Fatalf("%s imports %s — the relay must not be able to name what it forwards", path, banned)
					}
				}
			}
		}
	}
}
