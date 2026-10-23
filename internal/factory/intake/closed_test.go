package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestClosedJobCannotBeAutomaticallyStartedOrAsked(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "owner/repo", Kind: factory.KindIssue, Num: 9, State: factory.StateNew, DoneHow: "closed_locally", DoneAt: time.Now(), Labels: []string{"ready", "size:S"}})
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	reads, starts := 0, 0
	in := intake.New(st, intake.Options{Record: history, Load: func(string) (pipeline.File, error) { reads++; return pipeline.Parse("start:\n  labels: [ready]\n") }, Launch: func(int) error { starts++; return nil }})
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := history.Append(context.Background(), record.Event{Kind: record.ClosedLocally, Job: it.ID, Handle: "sam"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.DoneHow = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starts != 0 {
		t.Fatal("Reopen automatically restarted work")
	}
	_ = st.Update(it.ID, func(cur *factory.Item) error { cur.DoneHow = "closed_locally"; return nil })
	got, _ := st.Get(it.ID)
	if starts != 0 || got.DoneHow != "closed_locally" || got.Question != "" {
		t.Fatalf("closed job was considered: reads=%d starts=%d state=%s", reads, starts, got.State)
	}
}
