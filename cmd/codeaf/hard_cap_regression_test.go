package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestLiveStageSampleDoesNotLatchAcrossRaise(t *testing.T) {
	st, _ := chatSpendFloor(t)
	item, err := st.Create(factory.Item{Repo: "acme/api", Cap: .24, Stream: &factory.Stream{}})
	if err != nil {
		t.Fatal(err)
	}
	b := &stageBudget{floor: st, item: item}
	if b.RefusalWithSpend(.25) == "" {
		t.Fatal("fixture did not sample cap crossing")
	}
	// The watcher has sampled but has not performed StopWork yet.
	if err := st.Update(item.ID, func(cur *factory.Item) error { cur.Cap = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	if why := b.RefusalWithSpend(.25); why != "" {
		t.Fatalf("stale live sample survives raise before stop: %s", why)
	}
}
