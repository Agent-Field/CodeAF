package session

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoShareDeliverableRoadIsOverTheCeiling holds publication to the same
// fifteen-decision bar as the task engine, WITH NO LEDGER AT ALL. Publication
// was written after that ratchet was fitted, so it has no debt to name: every
// road in `share*.go` is split along its own phases instead.
func TestNoShareDeliverableRoadIsOverTheCeiling(t *testing.T) {
	files := 0
	forEachPackageFile(t, func(path string, file *ast.File, _ *token.FileSet) {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "share") {
			return
		}
		files++
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			if at := cyclomatic(function.Body); at > theCeiling {
				t.Errorf("%s: %s holds %d decisions and the ceiling is %d", base, functionName(function), at, theCeiling)
			}
		}
	})
	if files < 3 {
		t.Fatalf("only %d share*.go files were scanned; the walk is broken rather than clean", files)
	}
}
