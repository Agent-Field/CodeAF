package intake_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// W8: a labelled issue with pending waits never reaches intake's Launch.
func TestIntakeSkipsPendingWaits(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/api"}); err != nil {
		t.Fatal(err)
	}
	parent, err := st.Create(factory.Item{Repo: "owner/api", Num: 1, Kind: factory.KindIssue, State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	child, err := st.Create(factory.Item{Repo: "owner/api", Num: 2, Kind: factory.KindIssue, State: factory.StateNew, Tier: factory.TierOwner, Title: "Wait for the parent", Labels: []string{"agent-ready", "size:S"}, WaitsOn: []int{parent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	child, err = st.Get(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Parse("default_budget: 1\nstart:\n  labels: [agent-ready]\n")
	if err != nil {
		t.Fatal(err)
	}
	launched := 0
	in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Launch: func(id int) error { launched++; return nil }})
	for range 2 {
		if err := in.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	after, err := st.Get(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if launched != 0 || !reflect.DeepEqual(after, child) {
		t.Fatalf("pending job changed: launches = %d, job = %+v", launched, after)
	}
	if err := st.Update(child.ID, func(it *factory.Item) error { it.WaitsReleased = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if launched != 1 {
		t.Fatalf("released job launches = %d", launched)
	}
}
