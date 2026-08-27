package session

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
)

// ── THE BOUNDARY IS A BOUNDARY ──────────────────────────────────────────────
//
// These are structural tests, in this package's own convention (landing_test.go
// scans the sources with go/ast and holds the package to a law rather than to a
// behaviour). They exist because the failure the response boundary was written
// for was NOT a bug in any one site: it was three sites each answering "what did
// that failure mean" its own way, and a fourth site added next year would answer
// it a fourth way unless something says no.
//
// So: one file classifies, and one file buys a stronger model. A change that
// adds an escalation trigger somewhere else fails here with the line number.

// boundaryFile is the one file in this package allowed to ask the taxonomy
// anything or to buy a tier with the answer.
const boundaryFile = "taxonomy_boundary.go"

// tierBuyers are the functions that put a piece of work on a DEARER MODEL than
// the one it was on, and every one of them must be called from the boundary and
// from nowhere else.
//
//   - nextNodeModel walks the adapter's fallback chain for a node whose run
//     ended on a provider failure (task_run.go). It is the site that read four
//     consecutive transport 400s as the model being unable and moved a whole
//     task onto a model seven times the price.
//   - repairModel resolves the careful tier for a repair round (repair_role.go).
//     It is the site that bought that tier on the FIRST finding, whether or not
//     the round it judged had lost its calls on the wire.
//   - terminalProviderFailure is the test those two used to be gated on. It is
//     in the list because a caller that asked it directly would be classifying
//     by hand, which is the habit this whole mechanism replaces.
var tierBuyers = map[string]string{
	"nextNodeModel":           "task_run.go",
	"repairModel":             "repair_role.go",
	"terminalProviderFailure": "task_run.go",
}

// TestOnlyTheBoundaryClassifiesAFailure holds the package to ONE reader of the
// taxonomy. A site that called [taxonomy.Classify] for itself would get a
// verdict nothing journaled, which is the half of the record the money turns on
// (sessionfile.go's [journalFailure]).
func TestOnlyTheBoundaryClassifiesAFailure(t *testing.T) {
	fset, files := packageSources(t)
	var stray []string
	seen := 0
	for path, file := range files {
		base := filepath.Base(path)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Classify" {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "taxonomy" {
				return true
			}
			seen++
			if base != boundaryFile {
				stray = append(stray, fmt.Sprintf("%s:%d", path, fset.Position(call.Pos()).Line))
			}
			return true
		})
	}
	if seen < 1 {
		t.Fatalf("the scan found %d calls to taxonomy.Classify, so it has broken rather than passed", seen)
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		t.Fatalf("a failure was classified outside the response boundary:\n\t%s\n"+
			"Add a reader to %s instead: it is the one place that journals the verdict, "+
			"and a verdict nobody wrote down cannot be counted.",
			strings.Join(stray, "\n\t"), boundaryFile)
	}
}

// TestOnlyTheBoundaryBuysAStrongerModel is the law that would have caught the
// measured failure. Both purchases are now gated on a verdict, and a third one
// added anywhere else in this package fails here.
func TestOnlyTheBoundaryBuysAStrongerModel(t *testing.T) {
	fset, files := packageSources(t)
	var stray []string
	seen := map[string]int{}
	for path, file := range files {
		base := filepath.Base(path)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calledName(call.Fun)
			if _, watched := tierBuyers[name]; !watched {
				return true
			}
			seen[name]++
			if base != boundaryFile {
				stray = append(stray, fmt.Sprintf("%s:%d: %s", path, fset.Position(call.Pos()).Line, name))
			}
			return true
		})
	}
	for name, declared := range tierBuyers {
		if seen[name] == 0 {
			t.Fatalf("the scan found no call to %s (declared in %s), so it has broken rather than passed",
				name, declared)
		}
	}
	if len(stray) > 0 {
		sort.Strings(stray)
		t.Fatalf("a stronger model was bought outside the response boundary:\n\t%s\n"+
			"Route it through %s: nothing about who SERVED a request is evidence about who was ASKED, "+
			"and reading the two as one was 57–82%% of the bill on three measured runs.",
			strings.Join(stray, "\n\t"), boundaryFile)
	}
}

// TestEveryClassHasExactlyOnePolicy holds the registry to its own shape: three
// classes, three policies, and no class answered by a branch somebody wrote at a
// call site because the registry had nothing for it.
func TestEveryClassHasExactlyOnePolicy(t *testing.T) {
	want := []taxonomy.Class{taxonomy.Capability, taxonomy.Transport, taxonomy.Work}
	got := taxonomy.Registered()
	if len(got) != len(want) {
		t.Fatalf("registered classes = %v, want %v", got, want)
	}
	for i, class := range want {
		if got[i] != class {
			t.Fatalf("registered classes = %v, want %v", got, want)
		}
		policy, ok := taxonomy.PolicyFor(class)
		if !ok {
			t.Fatalf("no policy is registered for %s", class)
		}
		if policy.Class() != class {
			t.Fatalf("the policy filed under %s answers for %s", class, policy.Class())
		}
	}
}

// calledName is the name of whatever a call expression calls — `f`, `a.f`,
// `pkg.f` — so one scan covers a method on the agent and a plain function alike.
func calledName(fun ast.Expr) string {
	switch called := fun.(type) {
	case *ast.Ident:
		return called.Name
	case *ast.SelectorExpr:
		return called.Sel.Name
	}
	return ""
}
