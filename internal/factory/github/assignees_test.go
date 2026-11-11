package github

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func assigneeItem(set []string) factory.Item {
	return factory.Item{Product: "acme", Repo: "api", Num: 5, Kind: factory.KindIssue, Title: "t", Origin: factory.OriginForge, SourceAssignees: set}
}

func only(t *testing.T, st interface {
	List() ([]factory.Item, error)
}) factory.Item {
	t.Helper()
	items, err := st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	return items[0]
}

// The source maps the forge's list to a sorted, deduplicated, lowercase fact;
// absent stays unknown and an empty array stays known-none.
func TestSourceMapsAssigneesAsNormalizedFact(t *testing.T) {
	s := &Source{}
	got := s.issueItem(nil, "acme", "api", forge.Issue{Number: 1, Assignees: []string{"Zed", "@bob", "zed"}}).SourceAssignees
	if !reflect.DeepEqual(got, []string{"bob", "zed"}) {
		t.Fatalf("issue %#v", got)
	}
	if s.issueItem(nil, "acme", "api", forge.Issue{Number: 1}).SourceAssignees != nil {
		t.Fatal("an unread list became a fact")
	}
	if got := s.issueItem(nil, "acme", "api", forge.Issue{Number: 1, Assignees: []string{}}).SourceAssignees; got == nil || len(got) != 0 {
		t.Fatalf("known none lost: %#v", got)
	}
	if s.issueItem(nil, "acme", "api", forge.Issue{Number: 1, Assignees: []string{""}}).SourceAssignees != nil {
		t.Fatal("a login-less entry must be unknown, not none")
	}
	if got := s.pullItem(nil, "acme", "api", forge.Pull{Number: 2, Assignees: []string{"b", "a"}}, "").SourceAssignees; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("pull %#v", got)
	}
}

// Multiple assignees are stored whole. No execution owner is picked, and
// nothing about who may run the work changes.
func TestMergeStoresMultipleAssigneesWithoutPickingAnOwner(t *testing.T) {
	st := openStore(t)
	if err := Merge(st, Name, []factory.Item{assigneeItem([]string{"zed", "bob"})}); err != nil {
		t.Fatal(err)
	}
	it := only(t, st)
	if !reflect.DeepEqual(it.SourceAssignees, []string{"bob", "zed"}) || it.Assignee != "" || it.Taker != "" || it.Starter != "" || it.Person() != "" {
		t.Fatalf("stored %+v", it)
	}
}

// The same set in another order is not a change; presence and set are.
func TestMergeAssigneeOrderIsNotAChangeButPresenceIs(t *testing.T) {
	st := openStore(t)
	if err := Merge(st, Name, []factory.Item{assigneeItem([]string{"bob", "ann"})}); err != nil {
		t.Fatal(err)
	}
	first := only(t, st)
	reordered := assigneeItem([]string{"ann", "bob"})
	if forgeChanged(first, reordered) {
		t.Fatal("order alone counted as a change")
	}
	if forgeChanged(first, assigneeItem(nil)) {
		t.Fatal("an unread list counted as a change")
	}
	if !forgeChanged(first, assigneeItem([]string{"ann"})) {
		t.Fatal("a smaller set was not a change")
	}
	if err := Merge(st, Name, []factory.Item{assigneeItem(nil)}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(only(t, st).SourceAssignees, []string{"ann", "bob"}) {
		t.Fatal("an unread list wiped the fact")
	}
	// Unknown becomes known-none, and that is a change that lands.
	if err := Merge(st, Name, []factory.Item{assigneeItem([]string{})}); err != nil {
		t.Fatal(err)
	}
	if got := only(t, st).SourceAssignees; got == nil || len(got) != 0 {
		t.Fatalf("known none did not land: %#v", got)
	}
}

// A refresh of the fact never touches the execution owner, the lease proxy
// (machine and running state) or the item's state.
func TestMergeAssigneeRefreshLeavesTheExecutionOwnerAlone(t *testing.T) {
	st := openStore(t)
	seed := assigneeItem([]string{"ann"})
	seed.Taker, seed.Starter, seed.Assignee = "carol", "dan", "erin"
	seed.WorktreeMachine = "m1"
	if err := Merge(st, Name, []factory.Item{seed}); err != nil {
		t.Fatal(err)
	}
	before := only(t, st)
	if err := st.Update(before.ID, func(it *factory.Item) error { it.State = factory.StateRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	before = only(t, st)
	for _, set := range [][]string{{"zed", "bob"}, {}} {
		if err := Merge(st, Name, []factory.Item{assigneeItem(set)}); err != nil {
			t.Fatal(err)
		}
	}
	after := only(t, st)
	if after.Assignee != before.Assignee || after.Taker != before.Taker || after.Starter != before.Starter ||
		after.WorktreeMachine != before.WorktreeMachine || after.State != before.State {
		t.Fatalf("owner moved:\nbefore %+v\nafter  %+v", before, after)
	}
	if after.SourceAssignees == nil || len(after.SourceAssignees) != 0 {
		t.Fatalf("fact not refreshed: %#v", after.SourceAssignees)
	}
}

func TestNewItemOwnerIsOnlyTheSoleNormalizedAssignee(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  []string
		want string
	}{
		{"sole", []string{"@Sam"}, "sam"},
		{"duplicates of one", []string{"Sam", "sam"}, "sam"},
		{"several", []string{"ann", "sam"}, ""},
		{"known none", []string{}, ""},
		{"unknown", nil, ""},
	} {
		if got := factory.SoleSourceAssignee(tc.set); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	s := &Source{}
	if it := s.issueItem(nil, "acme", "api", forge.Issue{Number: 1, Assignees: []string{"Sam"}}); it.Assignee != "sam" {
		t.Fatalf("issue: %+v", it)
	}
	if it := s.pullItem(nil, "acme", "api", forge.Pull{Number: 2, Assignees: []string{"ann", "sam"}}, forge.CheckState("")); it.Assignee != "" {
		t.Fatalf("pull: %+v", it)
	}
}

// A later read never turns an existing, ownerless item into someone's: only
// creation derives the owner from a sole assignee.
func TestMergeUpdateNeverDerivesAnOwnerFromTheSet(t *testing.T) {
	st := openStore(t)
	if err := Merge(st, Name, []factory.Item{assigneeItem(nil)}); err != nil {
		t.Fatal(err)
	}
	in := assigneeItem([]string{"sam"})
	in.Assignee = "sam"
	if err := Merge(st, Name, []factory.Item{in}); err != nil {
		t.Fatal(err)
	}
	if got := only(t, st); got.Assignee != "" || !reflect.DeepEqual(got.SourceAssignees, []string{"sam"}) {
		t.Fatalf("update moved the owner: %+v", got)
	}
}

// A login-less entry handed straight to Merge (the setup scan does) must
// neither create a known none nor wipe a known set.
func TestBlankLoginIsNeverAProvenNone(t *testing.T) {
	st := openStore(t)
	if err := Merge(st, Name, []factory.Item{assigneeItem([]string{"ann"})}); err != nil {
		t.Fatal(err)
	}
	if err := Merge(st, Name, []factory.Item{assigneeItem([]string{""})}); err != nil {
		t.Fatal(err)
	}
	if got := only(t, st); !reflect.DeepEqual(got.SourceAssignees, []string{"ann"}) {
		t.Fatalf("blank entry rewrote the fact: %#v", got.SourceAssignees)
	}
	if got := factory.SoleSourceAssignee([]string{"", "bob"}); got != "" {
		t.Fatalf("partial list picked a sole owner %q", got)
	}
}
