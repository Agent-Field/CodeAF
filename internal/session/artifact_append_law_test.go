package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Every O_APPEND reference is accounted for, including package-level consts,
// vars and initializer closures. Flag aliases must originate inside the funnel
// or one of the separate stores, not in a shared declaration that lets a new
// deliverable writer bypass both locks.
func TestArtifactIndexHasOneAppendFunnel(t *testing.T) {
	funnels := 0
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		count, violations := artifactAppendViolations(path, file)
		funnels += count
		for _, pos := range violations {
			t.Errorf("%s: append flags outside appendArtifactIndex", fset.Position(pos))
		}
	})
	if funnels != 1 {
		t.Errorf("found %d append opens in the artifacts funnel, want exactly one", funnels)
	}
}

func artifactAppendViolations(path string, file *ast.File) (funnels int, violations []token.Pos) {
	otherStores := map[string]string{
		"answers.go":      "deliverAnswer",
		"groundladder.go": "hideFurrowMarker",
		"memory.go":       "appendMemoryFailureLog",
		"memoryledger.go": "appendMemoryRow",
		"plandb_plan.go":  "TaskGraph.planNote",
		"question.go":     "Agent.recordDecision",
		"renderlog.go":    "renderLedger.append",
		"sessionfile.go":  "openSessionFile",
		"task_index.go":   "appendTaskIndex",
		"usage_ledger.go": "openUsageLedger",
	}
	ast.Inspect(file, func(node ast.Node) bool {
		flag, ok := node.(*ast.Ident)
		if !ok || flag.Name != "O_APPEND" {
			return true
		}
		owner := enclosingFunction(file, flag.Pos())
		if path == "artifact_append.go" && owner == "appendArtifactIndex" {
			funnels++
		} else if owner == "" || otherStores[path] != owner {
			violations = append(violations, flag.Pos())
		}
		return true
	})
	return funnels, violations
}

func TestArtifactAppendGuardFixtures(t *testing.T) {
	t.Run("real_funnel", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "artifact_append.go", nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		funnels, violations := artifactAppendViolations("artifact_append.go", file)
		if funnels != 1 || len(violations) != 0 {
			t.Fatalf("real funnel: found %d funnel references and %d violations, want 1 and 0", funnels, len(violations))
		}
	})
	for _, name := range []string{"literal", "const_flags", "var_flags"} {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			packages, err := parser.ParseDir(fset, filepath.Join("testdata", "artifact_append_law", name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			violations := 0
			for _, pkg := range packages {
				for path, file := range pkg.Files {
					_, found := artifactAppendViolations(filepath.Base(path), file)
					violations += len(found)
				}
			}
			if violations != 1 {
				t.Fatalf("guard reported %d forbidden append references, want 1", violations)
			}
		})
	}
}
