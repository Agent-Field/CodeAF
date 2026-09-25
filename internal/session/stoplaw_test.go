package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// EVERY ROW PUBLISHED IN A STATE A STOP MEANS SOMETHING IN HAS AN OWNER A STOP
// CAN REACH.
//
// A surface offers a stop on any row that is queued or running, and sends the
// id the row wears ([Agent.Cancel]). That only works when whoever published the
// row also answers that id. A run under the bash belt published running rows
// wearing task numbers from outside the task graph, the only owner a task
// number had, and every stop a person pressed on one was refused while the run
// carried on (stoprun.go, measured 2026-09-19). A cancel kind with no caller and
// a publisher with no cancel are the same fault seen from either end, and
// nothing failed when it was written.
//
// So this is a LEDGER, the way the surface's door laws are: every function in
// this package that builds a row already queued or running is listed here with
// the cancel kind that reaches that row's owner and THE TEST THAT PROVES IT BY
// STOPPING ONE. A new publisher fails this law until it has both, and an entry
// whose proof has been deleted fails it too. The task graph's own nodes are not
// here because they are not built from a literal state: their notices copy the
// node's state, and the graph is the owner `task:N` has always reached.
var stoppableRowPublishers = map[string]struct{ kind, proof string }{
	"startOrJoinTaskRun":    {CancelTask, "TestAStopOnARunsOwnRowEndsTheRun"},
	"setBeltRunMachineHold": {CancelTask, "TestAStopOnARunsOwnRowEndsTheRun"},
	"ContinueRun":           {CancelTask, "TestAStopReachesARunThatWasCarriedOn"},
	"newOrchestrateFamily":  {CancelRun, "TestCancelStopsAnAdaptiveRun"},
	"sayForming":            {CancelRun, "TestCancelStopsAnAdaptiveRun"},
	"pauseRun":              {CancelRun, "TestCancelStopsAnAdaptiveRun"},
	"rename":                {CancelRun, "TestCancelStopsAnAdaptiveRun"},
}

// stoppableStates are the states a stop means something in, by the names this
// package spells them with.
var stoppableStates = map[string]bool{"TaskRunning": true, "TaskQueued": true}

func TestEveryPublisherOfARunningRowIsOneAStopCanReach(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]string{}
	tests := map[string]bool{}
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if strings.HasSuffix(name, "_test.go") {
				tests[fn.Name.Name] = true
				continue
			}
			if where := publishesStoppableRow(fset, fn); where != "" {
				found[fn.Name.Name] = where
			}
		}
	}
	if len(found) == 0 {
		t.Fatal("the law found no publisher at all, so it is reading nothing: the row type or its state field was renamed")
	}
	var missing, stale, unproven []string
	for name, where := range found {
		entry, listed := stoppableRowPublishers[name]
		switch {
		case !listed:
			missing = append(missing, name+" ("+where+")")
		case !tests[entry.proof]:
			unproven = append(unproven, name+" names "+entry.proof+", which is not a test in this package")
		}
	}
	for name := range stoppableRowPublishers {
		if _, still := found[name]; !still {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	sort.Strings(unproven)
	if len(missing) > 0 {
		t.Errorf("these functions publish a row a person can press stop on, and the ledger names no owner a stop reaches:\n  %s\n"+
			"add each to stoppableRowPublishers with the cancel kind that reaches its owner and a test that stops one", strings.Join(missing, "\n  "))
	}
	if len(unproven) > 0 {
		t.Errorf("these ledger entries name a proof that does not exist:\n  %s", strings.Join(unproven, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("these ledger entries publish no such row any more; delete them:\n  %s", strings.Join(stale, "\n  "))
	}
}

// publishesStoppableRow answers where a function builds a row in a state a stop
// means something in, and the empty string when it builds none. It is the one
// reader both tests below use, so the fixture proves this reader and not a copy.
func publishesStoppableRow(fset *token.FileSet, fn *ast.FuncDecl) string {
	where := ""
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || !namesType(lit.Type, "TaskNotice") {
			return true
		}
		for _, element := range lit.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, _ := pair.Key.(*ast.Ident)
			value, _ := pair.Value.(*ast.Ident)
			if key != nil && value != nil && key.Name == "State" && stoppableStates[value.Name] {
				where = fset.Position(lit.Pos()).String()
			}
		}
		return true
	})
	return where
}

// namesType reports whether a composite literal's type is the named one, bare
// or qualified.
func namesType(expr ast.Expr, name string) bool {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name == name
	case *ast.SelectorExpr:
		return typed.Sel.Name == name
	}
	return false
}

// THE LAW SEES THE FAULT IT WAS WRITTEN FOR: a publisher the ledger has never
// heard of is named, and one it knows is passed.
func TestTheStopLedgerLawNamesAnUnlistedPublisher(t *testing.T) {
	dir := t.TempDir()
	source := "package session\n\nfunc secondGraphPublishes() TaskNotice { return TaskNotice{ID: 1, State: TaskRunning} }\n\n" +
		"func settles() TaskNotice { return TaskNotice{ID: 1, State: TaskDone} }\n"
	path := filepath.Join(dir, "second.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var publishers []string
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && publishesStoppableRow(fset, fn) != "" {
			publishers = append(publishers, fn.Name.Name)
		}
	}
	if len(publishers) != 1 || publishers[0] != "secondGraphPublishes" {
		t.Fatalf("the law read %v, want only the function that publishes a running row", publishers)
	}
	if _, listed := stoppableRowPublishers["secondGraphPublishes"]; listed {
		t.Fatal("the fixture's publisher is in the real ledger")
	}
}
