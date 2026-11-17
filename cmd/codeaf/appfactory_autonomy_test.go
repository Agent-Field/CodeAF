package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestAppFactoryAutoManualControlUsesTypedReceiptsOnAttachedFactory(t *testing.T) {
	f := startActionFactory(t)
	owner := signedIn(t, f, 1, "alice")
	other := signedIn(t, f, 2, "bob")
	it := f.create(t, factory.Item{Title: "ordinary issue", Assignee: "alice", State: factory.StateNew, Autonomy: factory.AutonomyManual})
	for _, mode := range []string{"auto", "manual"} {
		out := owner.act(t, "factory.job_control", it.ID, map[string]any{"mode": mode})
		if out.Action != "set_"+mode || out.Current == nil || out.Current.Autonomy != mode {
			t.Fatalf("incorrect typed mode receipt: action=%s current=%+v", out.Action, out.Current)
		}
	}
	other.refused(t, "factory.job_control", other.params(t, it.ID, map[string]any{"mode": "auto"}), "forbidden")
	owner.refused(t, "factory.job_control", owner.params(t, it.ID, map[string]any{"mode": "unexpected"}), "bad_params")
	owner.refused(t, "factory.job_control", owner.params(t, it.ID, map[string]any{"mode": "auto", "words": "merge too"}), "bad_params")
}
