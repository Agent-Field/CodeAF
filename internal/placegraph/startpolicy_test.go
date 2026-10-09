package placegraph

import (
	"reflect"
	"testing"
)

func TestStartPolicyImaginesTheFilingWithoutTouchingTheSnapshot(t *testing.T) {
	st := &State{Version: 1, Places: []Place{
		{ID: "pl_a", Name: "A", Policy: Policy{Model: "m1"}},
		{ID: "pl_b", Name: "B", Parents: []string{"pl_a"}},
		{ID: "pl_c", Name: "C", Archived: true, Policy: Policy{Model: "m2"}},
	}}
	snap := newSnapshot(st)
	before := *snap.State.clone()
	d, ok := snap.StartPolicy("pl_b", PolicyModel)
	if !ok || d.Value != "m1" || d.Outcome != PolicyAgreed || d.DecidedBy != "pl_a" {
		t.Fatalf("child must inherit: %+v %v", d, ok)
	}
	if _, ok := snap.StartPolicy("pl_b", PolicyPermissions); ok {
		t.Fatal("no permissions opinion must report none")
	}
	if _, ok := snap.StartPolicy("pl_c", PolicyModel); ok {
		t.Fatal("an archived place cannot receive a chat")
	}
	if _, ok := snap.StartPolicy("pl_missing", PolicyModel); ok {
		t.Fatal("an unknown place cannot receive a chat")
	}
	if !reflect.DeepEqual(before, *snap.State.clone()) || len(snap.Memberships) != 0 {
		t.Fatal("the snapshot was changed")
	}
}
