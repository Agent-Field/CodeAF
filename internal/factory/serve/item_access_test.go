package serve

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// THE ACCESS CORE'S RULES, one row each: the owner holds full, a share gives
// its role to a person or a whole team and the highest one wins, a private
// item is its owner's alone, and the operator views what is not private and
// edits nothing through being the operator.
func TestTheAccessCoreRanksRolesFromOwnerSharesAndPrivacy(t *testing.T) {
	santosh := Viewer{Handle: "santosh", Teams: map[string]bool{"tm_platform": true}}
	abir := Viewer{Handle: "abir", Teams: map[string]bool{"tm_growth": true}}
	mia := Viewer{Handle: "mia", Teams: map[string]bool{}}
	operator := Viewer{Handle: "owner", Operator: true, Teams: map[string]bool{}}
	solo := Viewer{Handle: "", Solo: true, Operator: true, Teams: map[string]bool{}}
	person := func(id string) factory.WorkstreamOwner { return factory.WorkstreamOwner{Kind: "person", ID: id} }
	team := func(id string) factory.WorkstreamOwner { return factory.WorkstreamOwner{Kind: "team", ID: id} }
	shared := Item{Kind: ItemWorkstream, ID: "ws_1", Owner: person("santosh"), Shares: []factory.Share{
		{Kind: "team", ID: "tm_growth", Role: factory.RoleView},
		{Kind: "person", ID: "@Abir", Role: factory.RoleEdit},
		{Kind: "person", ID: "mia", Role: factory.RoleComment},
	}}
	private := Item{Kind: ItemWorkstream, ID: "ws_2", Owner: person("santosh"), Private: true,
		Shares: []factory.Share{{Kind: "person", ID: "abir", Role: factory.RoleFull}}}
	teamOwned := Item{Kind: ItemCanvas, ID: "cv_1", Owner: team("tm_platform")}
	for _, c := range []struct {
		name string
		v    Viewer
		it   Item
		want string
	}{
		{"the owner holds full", santosh, shared, factory.RoleFull},
		{"a person share beats a lower team share", abir, shared, factory.RoleEdit},
		{"a person share alone", mia, shared, factory.RoleComment},
		{"the operator views what is shared", operator, shared, factory.RoleView},
		{"the owner of a private item holds full", santosh, private, factory.RoleFull},
		{"a share on a private item grants nothing", abir, private, ""},
		{"the operator does not read a private item", operator, private, ""},
		{"the owning team holds full", santosh, teamOwned, factory.RoleFull},
		{"anyone else holds nothing", mia, teamOwned, ""},
		{"solo holds full on everything", solo, private, factory.RoleFull},
	} {
		if got := roleOf(c.v, c.it); got != c.want {
			t.Errorf("%s: roleOf = %q, want %q", c.name, got, c.want)
		}
	}
	if can(abir, shared, factory.RoleFull) || !can(abir, shared, factory.RoleComment) || can(mia, shared, factory.RoleEdit) {
		t.Fatal("can does not rank roles")
	}
	if can(santosh, shared, "admin") || can(santosh, shared, "") {
		t.Fatal("an unknown verb was allowed")
	}
	if !nameOnly(operator, private) || nameOnly(operator, shared) || nameOnly(abir, private) {
		t.Fatal("only the operator sees a private item's name, and only that")
	}
}

// AN ITEM'S EVENTS REACH WHO MAY VIEW IT: a private item's never reach the
// operator, and an audience joined from before and after reaches someone a
// change just took it from.
func TestItemAudienceFollowsAccess(t *testing.T) {
	operator := Viewer{Handle: "owner", Operator: true, Teams: map[string]bool{}}
	abir := Viewer{Handle: "abir", Teams: map[string]bool{"tm_growth": true}}
	before := Item{Owner: factory.WorkstreamOwner{Kind: "person", ID: "santosh"}, Shares: []factory.Share{{Kind: "team", ID: "tm_growth", Role: "view"}}}
	after := Item{Owner: before.Owner, Private: true}
	if operator.hears(audienceOf(after)) || abir.hears(audienceOf(after)) {
		t.Fatal("a private item's event reached someone else")
	}
	if !abir.hears(audienceOf(after, before)) || !operator.hears(audienceOf(after, before)) {
		t.Fatal("a share just taken away did not hear it go")
	}
	if a := audienceOf(after); !slices.Equal(a.Persons, []string{"santosh"}) {
		t.Fatalf("audience = %+v", a)
	}
}

// A WORKSTREAM KEPT BEFORE ROLES READS AS SHARED FOR EDIT: its teams-only
// shared_with comes back as edit shares, and is never written again.
func TestAWorkstreamSharedBeforeRolesReadsAsEditShares(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := `{"ws_1":{"id":"ws_1","name":"Webhooks","stage":"shaping","owner":{"kind":"person","id":"santosh"},"shared_with":["tm_growth"],"rev":3}}`
	if err := os.WriteFile(filepath.Join(st.Root(), store.WorkstreamsFile), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, ok, err := st.Workstream("ws_1")
	if err != nil || !ok {
		t.Fatalf("read = %v %v", ok, err)
	}
	want := []factory.Share{{Kind: "team", ID: "tm_growth", Role: factory.RoleEdit}}
	if !slices.Equal(ws.Shares, want) || ws.SharedWith != nil {
		t.Fatalf("folded = %+v / %v", ws.Shares, ws.SharedWith)
	}
}
