package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func openLadderStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "ladder.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

// TestRoleLadderWithoutAPlanKnobWritesNothing is the identical-behaviour claim
// for every machine that has not configured anything: the floor is installed on
// the handle, the journal does not grow, and every role still resolves to the
// same models the surface built its clients from.
func TestRoleLadderWithoutAPlanKnobWritesNothing(t *testing.T) {
	graph := openLadderStore(t)
	installRoleLadder(graph, "talk/model", "work/model", "work/model", "", "")

	bindings, err := graph.RoleBindings()
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 {
		t.Fatalf("an unconfigured launch bound %+v, want nothing", bindings)
	}
	want := map[store.ModelRole]string{
		store.RoleOrchestrate: "talk/model",
		store.RolePlan:        "work/model",
		store.RoleWork:        "work/model",
		// No cheap slot exists in configuration, so the skeptic and the clerk
		// stand on the work model rather than on nothing.
		store.RoleVerify: "work/model",
		store.RoleScribe: "work/model",
	}
	for role, model := range want {
		resolved, err := graph.ResolveRole(role, "")
		if err != nil {
			t.Fatalf("resolve %s: %v", role, err)
		}
		if resolved.Model != model || resolved.Source != store.RoleFromDefault {
			t.Fatalf("resolve %s = %+v, want %q from the compiled-in default", role, resolved, model)
		}
	}
}

// TestPlanKnobSeedsTheGlobalPlanBindingOnce covers the bridge: the environment
// initializes the plan role on first launch, says nothing on the next launch,
// and is named in the origin so the store knows an initializer wrote it.
func TestPlanKnobSeedsTheGlobalPlanBindingOnce(t *testing.T) {
	graph := openLadderStore(t)
	for launch := 0; launch < 3; launch++ {
		installRoleLadder(graph, "talk/model", "planning/model", "work/model", "planning/model", "")
	}
	binding, found, err := graph.RoleBindingAt(store.RolePlan, store.ScopeGlobal)
	if err != nil || !found {
		t.Fatalf("plan binding: found=%t err=%v", found, err)
	}
	if binding.Value != "planning/model" {
		t.Fatalf("plan binding = %+v, want the environment's model", binding)
	}
	if binding.Origin != store.RoleSeedOriginPrefix+"AFORGE_PLAN_MODEL" {
		t.Fatalf("origin = %q, want the environment named as an initializer", binding.Origin)
	}
	journal, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := 0
	for _, event := range journal {
		if event.Kind == store.EventRoleBindingSet {
			events++
		}
	}
	if events != 1 {
		t.Fatalf("three launches journaled %d bindings, want 1", events)
	}

	// The flag names itself instead, and a value the initializer has not said
	// before does move the binding.
	installRoleLadder(graph, "talk/model", "flagged/model", "work/model", "flagged/model", "flagged/model")
	binding, _, err = graph.RoleBindingAt(store.RolePlan, store.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Value != "flagged/model" || binding.Origin != store.RoleSeedOriginPrefix+"--plan-model" {
		t.Fatalf("flagged binding = %+v", binding)
	}
}
