package plandbtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// THE COMMIT GATE NEVER SHIPS. Importing this package registers a hook on every
// SQLite connection the process opens, so only a _test.go file may import it:
// a shipped file that did would put the gate's hook into the binary.
func TestNoShippedFileImportsTheCommitGate(t *testing.T) {
	const self = "github.com/Agent-Field/codeaf/internal/plandb/plandbtest"
	root := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// Hidden folders hold other checkouts and tool state, never this
			// module's shipped sources.
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, spec := range file.Imports {
			if imported, _ := strconv.Unquote(spec.Path.Value); imported == self {
				t.Errorf("%s imports the commit gate; only a _test.go file may", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
