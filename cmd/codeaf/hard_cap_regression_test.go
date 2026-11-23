package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestRecordedStageSampleDoesNotLatchAcrossRaise(t *testing.T) {
	st, _ := chatSpendFloor(t)
	item, err := st.Create(factory.Item{Repo: "acme/api", Cap: .24, Stream: &factory.Stream{}})
	if err != nil {
		t.Fatal(err)
	}
	b := &stageBudget{floor: st, item: item}
	b.Spend(.25)
	sampled, err := st.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if factoryrun.ChatRefusal(sampled) == "" {
		t.Fatal("fixture did not sample recorded cap crossing")
	}
	// The recorded crossing was sampled before the person raised the cap.
	if err := st.Update(item.ID, func(cur *factory.Item) error { cur.Cap = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	if why := b.Refusal(); why != "" {
		t.Fatalf("stale recorded sample survives raise before stop: %s", why)
	}
}
