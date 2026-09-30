package relayserve

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// L0, EXTENDED (contract §0). The packages that carry a person's data over the
// wire must not be able to name the packages that hold what a session, a cell
// or a vault keeps in the clear. A package that cannot import a type cannot
// decode one, so the ban is read out of the source and not out of a comment.
func TestRelayImportLaw(t *testing.T) {
	const module = "github.com/Agent-Field/codeaf/internal/"
	banned := map[string]bool{}
	for _, name := range []string{"remote", "pair", "session", "cell", "cellstore", "keys", "tui3"} {
		banned[module+name] = true
	}
	for _, dir := range []string{
		"../blobstore", "../directory", "../reqsign", "../wireauth", "../pairbox", ".", "../../cmd/relay",
	} {
		checkDir(t, dir, banned)
	}
}

func checkDir(t *testing.T, dir string, banned map[string]bool) {
	t.Helper()
	set := token.NewFileSet()
	pkgs, err := parser.ParseDir(set, dir, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("%s holds no package; the law would check nothing", dir)
	}
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			for _, imp := range file.Imports {
				if p := strings.Trim(imp.Path.Value, `"`); banned[p] {
					t.Errorf("%s imports %s", filepath.ToSlash(path), p)
				}
			}
		}
	}
}
