package store

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A GROUP POLICY IS KEPT WHOLE: none at first, each save replaces it and
// counts its version up, a policy the factory would not keep is refused whole
// with the old one still there, and it survives the store being opened again.
func TestGroupPoliciesAreKeptWhole(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Policy("ws"); ok || err != nil {
		t.Fatalf("a fresh factory keeps a policy: %v %v", ok, err)
	}
	a := factory.Allowance{ID: "ga_1", Jobs: []int{2, 3}, CapUSD: 10, EndsAt: "2026-11-20T00:00:00Z"}
	saved, err := st.ChangePolicy("ws", func(p *factory.GroupPolicy) error {
		p.Owner, p.Allowances = "alice", []factory.Allowance{a}
		return nil
	})
	if err != nil || saved.Version != 1 || saved.Group != "ws" || len(saved.Allowances) != 1 {
		t.Fatalf("saved %+v %v", saved, err)
	}
	if _, err := st.ChangePolicy("ws", func(p *factory.GroupPolicy) error {
		p.Allowances = append(p.Allowances, factory.Allowance{ID: "ga_1", Jobs: []int{9}, CapUSD: 1, EndsAt: "2026-11-20T00:00:00Z"})
		return nil
	}); err == nil {
		t.Fatal("a policy with one id twice was kept")
	}
	saved, err = st.ChangePolicy("ws", func(p *factory.GroupPolicy) error { p.Stopped, p.Allowances = true, nil; return nil })
	if err != nil || saved.Version != 2 || !saved.Stopped || saved.Allowances == nil || len(saved.Allowances) != 0 || saved.Owner != "alice" {
		t.Fatalf("a stop saved %+v %v", saved, err)
	}
	if _, err := st.ChangePolicy("other", func(p *factory.GroupPolicy) error { return nil }); err != nil {
		t.Fatal(err)
	}
	st, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.Policies()
	if err != nil || len(all) != 2 || all["ws"].Version != 2 || !all["ws"].Stopped {
		t.Fatalf("after reopening: %+v %v", all, err)
	}
	items, err := st.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("the policies file was read as a job: %+v %v", items, err)
	}
}
