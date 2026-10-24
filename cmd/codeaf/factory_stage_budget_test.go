package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
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
