package session

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactoryToolPersonalJobRuns(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agent := newFactoryAgent(t, st, 0)
	questions, stop := agent.WatchQuestions()
	defer stop()
	results := runFactoryAdd(t, agent, context.Background())
	q := awaitFactoryQuestion(t, questions)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionFactory, Ref: q.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	_ = awaitResult(t, results)
	items, err := st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v error=%v", items, err)
	}
	id := items[0].ID
	if err := st.Update(id, func(it *factory.Item) error {
		it.Stages = []factory.Stage{{Name: "test", Kind: factory.StageCheck, On: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ran := make(chan struct{}, 1)
	r := run.New(run.Options{Store: st, Exec: map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
		ran <- struct{}{}
		return factory.StageResult{Done: true}, nil
	})}})
	if err := r.Launch(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("personal job did not run")
	}
	for deadline := time.Now().Add(5 * time.Second); r.Running(id) && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if r.Running(id) {
		t.Fatal("personal job did not finish")
	}
	saved, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Failure != nil || saved.State != factory.StateLanded {
		t.Fatalf("personal job=%s refusal=%+v", saved.State, saved.Failure)
	}
}
