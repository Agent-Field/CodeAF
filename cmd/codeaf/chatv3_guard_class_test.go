package main

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

// TestV3ProcessGuardGoClass is the inventory law for goroutines started by a
// v3 process or launch. A long-lived writer must identify both its stop and the
// closeAll join. The three known-open rows name the later cells that own them;
// deleting a row is part of landing that cell.
func TestV3ProcessGuardGoClass(t *testing.T) {
	type class struct {
		kind, stop, join, owner string
	}
	want := map[string]class{
		"chatv3/standing":         {kind: "joined writer", stop: "v3Process.standingStop", join: "v3Process.closeAll calls stopStandingTicks"},
		"chatv3/standing-stop":    {kind: "joined helper", stop: "standingStop is closed", join: "chatv3/standing closes standingDone after this helper returns"},
		"pool/judge-sweep":        {kind: "joined writer", stop: "pool errand context", join: "v3Process.closeAll calls stopPoolErrands"},
		"chatv3/models":           {kind: "KNOWN-OPEN writer", owner: "later model-warm ownership cell (class target two)"},
		"engine/models":           {kind: "KNOWN-OPEN writer", owner: "later model-warm ownership cell (class target two)"},
		"chatv3/sweep-home":       {kind: "KNOWN-OPEN writer", owner: "later home-sweep ownership cell (class target three)"},
		"chatv3/background":       {kind: "one-shot", stop: "repairBackgroundChecks returns after one bounded Drift/Install pass"},
		"chatv3/once-questions":   {kind: "one-shot", stop: "agent.Close closes the WatchQuestions channel consumed by the range"},
		"chatv3/close-agent":      {kind: "joined one-shot", stop: "Agent.Close is bounded", join: "v3Process.closeAll waits on waiting"},
		"chatv3/host-reap":        {kind: "one-shot", stop: "exec.Cmd.Wait returns when the replaced ssh child exits"},
		"chatv3/host-follow":      {kind: "one-shot", stop: "remote Follow channel closes with the host connection"},
		"chatv3/telemetry-events": {kind: "one-shot", stop: "source event channel closes and countedEvents returns"},
	}

	got := processGuardScopes(t)
	for scope, pos := range got {
		c, ok := want[scope]
		if !ok {
			t.Errorf("new v3 process guard.Go %q at %s is unclassified: name its stop and closeAll join, or add a KNOWN-OPEN owner", scope, pos)
			continue
		}
		if strings.Contains(c.kind, "KNOWN-OPEN") {
			if c.owner == "" {
				t.Errorf("%s: KNOWN-OPEN entry has no owning later cell", scope)
			}
		} else if strings.Contains(c.kind, "joined") && (c.stop == "" || c.join == "") {
			t.Errorf("%s: joined entry must name both stop and closeAll join", scope)
		}
		delete(want, scope)
	}
	if len(want) != 0 {
		missing := make([]string, 0, len(want))
		for scope := range want {
			missing = append(missing, scope)
		}
		sort.Strings(missing)
		t.Fatalf("classified v3 process guards disappeared from the source inventory: %v", missing)
	}
}

func processGuardScopes(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"engine.go", "telemetry_events.go"}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "chatv3") && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, name)
		}
	}
	fset := token.NewFileSet()
	out := make(map[string]string)
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || (pkg.Name == "guard" && sel.Sel.Name != "Go") || (pkg.Name == "proc" && sel.Sel.Name != "warmModels") {
				return true
			}
			if pkg.Name != "guard" && pkg.Name != "proc" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true // generic helpers are classified at their literal call sites
			}
			scope := strings.Trim(lit.Value, "\"")
			pos := fset.Position(call.Pos())
			out[scope] = filepath.ToSlash(pos.String())
			return true
		})
	}
	return out
}
