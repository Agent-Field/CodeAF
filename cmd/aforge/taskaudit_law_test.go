package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

type taskAuditLawLocation struct {
	file string
	line int
}

// TestEverySessionDoorSharesOneTaskAuditReader states the one-reader law and
// holds every door to it. #618 was askable at all only because nothing said
// where a run's audit posture came from: a new door could build its own
// session.Config, or a second setting reader could quietly disagree with the
// first. The tree itself is the roster so a door added tomorrow joins the law
// without a filename somebody has to remember to add here.
func TestEverySessionDoorSharesOneTaskAuditReader(t *testing.T) {
	cmdRoot := filepath.Clean("..")
	fset := token.NewFileSet()
	var assignments []taskAuditLawLocation
	var readers []taskAuditLawLocation
	doors := 0

	err := filepath.WalkDir(cmdRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		parsed, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cmdRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hasSessionConfig := false
		reachesGovernance := false

		ast.Inspect(parsed, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				for _, target := range node.Lhs {
					selector, ok := target.(*ast.SelectorExpr)
					if ok && selector.Sel.Name == "TaskAudit" {
						position := fset.Position(selector.Sel.Pos())
						assignments = append(assignments, taskAuditLawLocation{rel, position.Line})
					}
				}
			case *ast.KeyValueExpr:
				key, ok := node.Key.(*ast.Ident)
				if ok && key.Name == "TaskAudit" {
					position := fset.Position(key.Pos())
					assignments = append(assignments, taskAuditLawLocation{rel, position.Line})
				}
			case *ast.CompositeLit:
				// A LITERAL WITH NO FIELDS IS NOT A DOOR. `return
				// session.Config{}, err` is the zero value handed back on an
				// error road — nothing is being configured there, and reading it
				// as a door would fail this law on files that build no run at
				// all. What a door looks like is a literal that sets something.
				if len(node.Elts) == 0 {
					return true
				}
				selector, ok := node.Type.(*ast.SelectorExpr)
				packageName, namedPackage := selectorX(selector)
				if ok && namedPackage && packageName == "session" && selector.Sel.Name == "Config" {
					hasSessionConfig = true
				}
			case *ast.ValueSpec:
				// And the other spelling of the same act: a `var cfg
				// session.Config` filled in field by field afterwards. It builds
				// a run's config exactly as the literal does, so the law has to
				// see it or the hole is one keyword wide.
				selector, ok := node.Type.(*ast.SelectorExpr)
				packageName, namedPackage := selectorX(selector)
				if ok && namedPackage && packageName == "session" && selector.Sel.Name == "Config" {
					hasSessionConfig = true
				}
			case *ast.CallExpr:
				if name, ok := node.Fun.(*ast.Ident); ok && name.Name == "applyV3Governance" {
					reachesGovernance = true
				}
				selector, ok := node.Fun.(*ast.SelectorExpr)
				packageName, namedPackage := selectorX(selector)
				if ok && namedPackage && packageName == "config" && selector.Sel.Name == "TaskAuditEnabledAt" {
					position := fset.Position(selector.Sel.Pos())
					readers = append(readers, taskAuditLawLocation{rel, position.Line})
				}
			}
			return true
		})

		if hasSessionConfig {
			doors++
			if !reachesGovernance {
				t.Errorf("%s builds a session.Config and never reaches applyV3Governance, so its run takes whatever audit posture the zero value happens to be", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the cmd tree: %v", err)
	}
	if doors == 0 {
		t.Fatal("no door builds a session.Config any more, so this law just passed without reading anything")
	}
	if len(assignments) != 1 || assignments[0].file != "aforge/chatv3.go" {
		t.Errorf("TaskAudit is assigned in more than one place under cmd/, so two doors can disagree about whether work is checked; it belongs once, in aforge/chatv3.go — found %s",
			taskAuditLawLocations(assignments))
	}
	if len(readers) != 1 || readers[0].file != "aforge/chatv3.go" {
		t.Errorf("config.TaskAuditEnabledAt is read in more than one place under cmd/; the row has one reader, in aforge/chatv3.go — found %s",
			taskAuditLawLocations(readers))
	}
}

func selectorX(selector *ast.SelectorExpr) (string, bool) {
	if selector == nil {
		return "", false
	}
	name, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return name.Name, true
}

func taskAuditLawLocations(locations []taskAuditLawLocation) string {
	if len(locations) == 0 {
		return "none"
	}
	lines := make([]string, 0, len(locations))
	for _, location := range locations {
		lines = append(lines, token.Position{Filename: location.file, Line: location.line}.String())
	}
	return strings.Join(lines, ", ")
}

// TestApplyV3GovernanceReadsTheTaskAuditRow holds both postures at the one
// reader: an explicit off reaches the session as false, while an unwritten row
// keeps the on-by-default promise. The session engine's own tests hold the
// sentence a task prints when this value is false.
func TestApplyV3GovernanceReadsTheTaskAuditRow(t *testing.T) {
	off, err := applyV3Governance(session.Config{},
		v3Profile(t, map[string]any{"task.audit": "off"}), false, false)
	if err != nil {
		t.Fatalf("reading a profile with task.audit off: %v", err)
	}
	if off.TaskAudit {
		t.Fatal("task.audit off reached the session as on")
	}

	on, err := applyV3Governance(session.Config{}, t.TempDir(), false, false)
	if err != nil {
		t.Fatalf("reading a profile that never wrote task.audit: %v", err)
	}
	if !on.TaskAudit {
		t.Fatal("an unwritten task.audit row did not keep the on default")
	}
}
