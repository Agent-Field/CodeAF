package factory

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// NESTING IS ONE LEVEL, both ways round, and a child's people are mirrored
// from its parent in the save that changed them.
func TestNestingIsOneLevelAndChildrenMirrorPeople(t *testing.T) {
	all := OrgWorkstreams{
		"p": {ID: "p", Owner: WorkstreamOwner{Kind: "person", ID: "santosh"}, Shares: []Share{{Kind: "team", ID: "eng", Role: "edit"}}},
		"c": {ID: "c", Parent: "p", Owner: WorkstreamOwner{Kind: "person", ID: "santosh"}},
		"x": {ID: "x"},
	}
	if err := CheckParent(all, "new", "c"); !errors.Is(err, ErrNestedTooDeep) {
		t.Fatalf("inside a child = %v", err)
	}
	if err := CheckParent(all, "p", "x"); !errors.Is(err, ErrHasChildren) {
		t.Fatalf("a parent moving inside = %v", err)
	}
	if err := CheckParent(all, "x", "p"); err != nil {
		t.Fatal(err)
	}
	more := InheritPeople(all, []string{"p"})
	if !slices.Equal(more, []string{"c"}) || !slices.Equal(all["c"].Shares, all["p"].Shares) {
		t.Fatalf("mirrored %v shares %+v", more, all["c"].Shares)
	}
	if got := ReadsFilesOf(all["c"]); !slices.Equal(got, []string{"c", "p"}) {
		t.Fatalf("a child reads %v", got)
	}
}

// The rules are kept clean, and zero is never start without asking.
func TestRulesAreKeptCleanAndZeroTakesTheAllowanceAway(t *testing.T) {
	r := WorkstreamRules{DefaultOwner: RulesOwner{Kind: "person", ID: "@Santosh"}, KeepsRunning: KeepsRunning{Orders: []string{" a ", "a", "b"}}}
	if err := CleanRules(&r); err != nil || r.DefaultOwner.ID != "santosh" || !slices.Equal(r.KeepsRunning.Orders, []string{"a", "b"}) {
		t.Fatalf("clean = %+v %v", r, err)
	}
	r.StartWithoutAskingUSD = -1
	if CleanRules(&r) == nil {
		t.Fatal("a negative amount was kept")
	}
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	p := GroupPolicy{Allowances: []Allowance{{ID: "mine", Jobs: []int{9}, CapUSD: 1, EndsAt: now.Format(time.RFC3339)}}}
	if !ApplyStartWithoutAsking(&p, []int{1, 2}, 5, "santosh", now) || len(p.Allowances) != 2 || p.Allowances[1].PerJobUSD != 5 || p.Allowances[1].CapUSD != 10 {
		t.Fatalf("policy = %+v", p)
	}
	if ApplyStartWithoutAsking(&p, []int{1, 2}, 5, "santosh", now.Add(time.Hour*3)) {
		t.Fatal("the same rule written again changed the policy")
	}
	if !ApplyStartWithoutAsking(&p, []int{1, 2}, 0, "santosh", now) || len(p.Allowances) != 1 || p.Allowances[0].ID != "mine" {
		t.Fatalf("zero kept %+v", p.Allowances)
	}
	if err := CheckAllowances([]Allowance{{ID: "a", Jobs: []int{1}, CapUSD: 1, PerJobUSD: 2, EndsAt: now.Format(time.RFC3339)}}); err == nil {
		t.Fatal("a per-job cap above the cap was kept")
	}
}
