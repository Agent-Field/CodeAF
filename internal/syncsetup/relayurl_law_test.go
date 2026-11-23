package syncsetup_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// ONE PLACE SPELLS THE THREE HOSTED RELAY ADDRESSES. Every command, pairing
// link and fallback reads them from internal/syncsetup/relayurl.go so two
// packages can never drift onto different relays.
func TestOnlyRelayURLSpellsTheHostedRelayAddresses(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(root, "internal", "syncsetup", "relayurl.go")
	needles := []string{
		syncsetup.ProductionRelayURL,
		strings.TrimPrefix(syncsetup.ProductionRelayURL, "https://"),
		syncsetup.StagingRelayURL,
		strings.TrimPrefix(syncsetup.StagingRelayURL, "https://"),
		syncsetup.DevRelayURL,
		strings.TrimPrefix(syncsetup.DevRelayURL, "https://"),
	}

	fset := token.NewFileSet()
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || path == owner {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, needle := range needles {
					if strings.Contains(val, needle) {
						rel, _ := filepath.Rel(root, path)
						pos := fset.Position(lit.Pos())
						t.Errorf("%s:%d spells %q; only internal/syncsetup/relayurl.go may spell the hosted relay addresses", rel, pos.Line, needle)
						break
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
