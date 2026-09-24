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
// closeAll join. chatv3/models and engine/models name the process waiter, joined
// since #1179, and the internal/catalog warm each one starts, which closeAll now
// cancels and joins through Models.Close. chatv3/sweep-home is joined too, by
// stopPlaceSweep since #1276. No row is known-open now; a new one that is must
// name the later cell that owns it, and deleting it is part of landing that cell.
func TestV3ProcessGuardGoClass(t *testing.T) {
	type class struct {
		kind, stop, join, owner string
	}
	want := map[string]class{
		"chatv3/standing":         {kind: "joined writer", stop: "v3Process.standingStop", join: "v3Process.closeAll calls stopStandingTicks"},
		"chatv3/standing-stop":    {kind: "joined helper", stop: "standingStop is closed", join: "chatv3/standing closes standingDone after this helper returns"},
		"pool/judge-sweep":        {kind: "joined writer", stop: "pool errand context", join: "v3Process.closeAll calls stopPoolErrands"},
		"chatv3/models":           {kind: "joined writer", stop: "warmModels waiter on the pool errand context, and the catalog's own warm context", join: "v3Process.closeAll calls stopPoolErrands for the waiter (since #1179) and Models.Close, which cancels and joins the internal/catalog warm"},
		"engine/models":           {kind: "joined writer", stop: "warmModels waiter on the pool errand context, and the catalog's own warm context", join: "v3Process.closeAll calls stopPoolErrands for the waiter (since #1179) and Models.Close, which cancels and joins the internal/catalog warm"},
		"chatv3/sweep-home":       {kind: "joined writer", stop: "v3Process.sweepCancel cancels the walk's context, checked between entries and before every destructive operation", join: "v3Process.closeAll calls stopPlaceSweep, which waits on sweepDone (since #1276)"},
		"chatv3/background":       {kind: "one-shot", stop: "repairBackgroundChecks returns after one bounded Drift/Install pass"},
		"chatv3/once-questions":   {kind: "one-shot", stop: "agent.Close closes the WatchQuestions channel consumed by the range"},
		"chatv3/close-agent":      {kind: "joined one-shot", stop: "Agent.Close is bounded", join: "v3Process.closeAll waits on waiting"},
		"chatv3/payment-credits":  {kind: "joined one-shot", stop: "processCtx is canceled and credits.Read is bounded", join: "v3Process.closeAll calls creditWatcher.close and waits"},
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
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "poolErrandGoCtx" {
				lit, ok := call.Args[1].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				out[strings.Trim(lit.Value, "\\\"")] = filepath.ToSlash(fset.Position(call.Pos()).String())
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
