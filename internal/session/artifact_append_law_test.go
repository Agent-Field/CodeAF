package session

import (
	"go/ast"
	"go/token"
	"testing"
)

// Every append open is accounted for, including code not reached by today's
// callers. Only the funnel may open the artifacts index for append. The other
// entries are separate stores, not exceptions for deliverable writers.
func TestArtifactIndexHasOneAppendFunnel(t *testing.T) {
	otherStores := map[string]string{
		"answers.go":      "deliverAnswer",
		"groundladder.go": "hideFurrowMarker",
		"memory.go":       "appendMemoryFailureLog",
		"memoryledger.go": "appendMemoryRow",
		"plandb_plan.go":  "planNote",
		"question.go":     "recordDecision",
		"renderlog.go":    "append",
		"sessionfile.go":  "openSessionFile",
		"task_index.go":   "appendTaskIndex",
		"usage_ledger.go": "openUsageLedger",
	}
	funnels := 0
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(node ast.Node) bool {
				flag, ok := node.(*ast.SelectorExpr)
				if !ok || flag.Sel.Name != "O_APPEND" {
					return true
				}
				if path == "artifact_append.go" && fn.Name.Name == "appendArtifactIndex" {
					funnels++
				} else if otherStores[path] != fn.Name.Name {
					t.Errorf("%s: %s opens for append outside appendArtifactIndex", fset.Position(flag.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	})
	if funnels != 1 {
		t.Errorf("found %d append opens in the artifacts funnel, want exactly one", funnels)
	}
}
