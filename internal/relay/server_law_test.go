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
// The cell packages are on the list for the same reason: they hold what a
// frame would reveal (turns and receipts, the vault's secrets, the tool
// inventory, the call log, session ids), so a relay that could name them
// could read or write a cell.
func TestTheRelayCannotEvenNameTheThingsItCarries(t *testing.T) {
	const module = "github.com/Agent-Field/codeaf/internal/"
	var forbidden []string
	for _, name := range []string{
		"remote", "pair", "session",
		"cell", "cellstore", "cellbudget", "cellindex", "keys",
		"executor", "inventory", "preflight", "sessionid",
	} {
		forbidden = append(forbidden, module+name)
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
