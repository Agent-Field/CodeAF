package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"os"
	"testing"
)

func TestStageBudgetBooksLiveAndReadsChangedCap(t *testing.T) {
	st, _ := chatSpendFloor(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Cap: 5, Stream: &factory.Stream{Spent: 4}})
	if err != nil {
		t.Fatal(err)
	}
	b := &stageBudget{floor: st, item: it}
	b.Spend(0.5)
	got, _ := st.Get(it.ID)
	if factory.JobSpent(got) != 4.5 || b.total() != 0.5 || b.Refusal() != "" {
		t.Fatalf("budget %+v total %v refusal %s", got, b.total(), b.Refusal())
	}
	_ = st.Update(it.ID, func(cur *factory.Item) error { cur.Cap = 4.5; return nil })
	if b.Refusal() == "" {
		t.Fatal("lowered cap was not enforced")
	}
	_ = st.Update(it.ID, func(cur *factory.Item) error { cur.Cap = 10; return nil })
	if b.Refusal() == "" {
		t.Fatal("an interrupted round lost its stop when the cap rose")
	}
	b = &stageBudget{floor: st, item: it}
	b.Spend(0.5)
	got, _ = st.Get(it.ID)
	if factory.JobSpent(got) != 5 || b.total() != 0.5 {
		t.Fatal("live spending was not recorded exactly once")
	}
}
func TestStageBudgetStopsOnUnavailableAccounting(t *testing.T) {
	b := &stageBudget{}
	if b.Refusal() == "" {
		t.Fatal("no store admitted spending")
	}
	b.Spend(1)
	if b.Refusal() == "" || b.total() != 1 {
		t.Fatal("failed receipt did not stop work")
	}
}

func TestStageBudgetReadOutageCanRecoverWithoutReplacingMeter(t *testing.T) {
	st, _ := chatSpendFloor(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Cap: 5, Stream: &factory.Stream{Spent: 1}})
	if err != nil {
		t.Fatal(err)
	}
	budget := &stageBudget{floor: st, item: it}
	path := st.ItemPath(it.ID)
	saved := path + ".unread"
	if err := os.Rename(path, saved); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(saved, path) }()
	if budget.Refusal() == "" {
		t.Fatal("unknown budget admitted paid work")
	}
	if err := os.Rename(saved, path); err != nil {
		t.Fatal(err)
	}
	if why := budget.Refusal(); why != "" {
		t.Fatalf("temporary read error permanently stopped the meter: %s", why)
	}
}

// Every live agent of a round asks Refusal ten times a second. An unchanged
// job document answers as it did, and a change to it is read on the next ask.
func TestStageBudgetRefusalReadsTheDocumentOnlyWhenItChanged(t *testing.T) {
	st, _ := chatSpendFloor(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Cap: 5, Labels: []string{"a", "b", "c", "d", "e", "f"}, Stream: &factory.Stream{Spent: 1}})
	if err != nil {
		t.Fatal(err)
	}
	b := &stageBudget{floor: st, item: it}
	if b.Refusal() != "" {
		t.Fatal("an unspent job refused")
	}
	if n := testing.AllocsPerRun(20, func() { _ = b.Refusal() }); n > 8 {
		t.Fatalf("an unchanged document cost %v allocations an ask; it was decoded again", n)
	}
	_ = st.Update(it.ID, func(cur *factory.Item) error { cur.Cap = 1; return nil })
	if b.Refusal() == "" {
		t.Fatal("a lowered cap was not read on the next ask")
	}
}
