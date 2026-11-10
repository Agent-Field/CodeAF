package run

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func afterClone(t *testing.T, rules factory.StartRules, read func(context.Context, factory.Item) (pipeline.File, error)) factory.Item {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stages := factory.DefaultRecipe().For(factory.KindIssue)
	id, err := st.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "x", Kind: factory.KindIssue, Stages: stages, Cap: factory.DefaultJobBudget, StartRules: &rules})
	if err != nil {
		t.Fatal(err)
	}
	var seen factory.Item
	r := New(Options{Store: st, RulesAfterClone: read})
	lp := &floorLoop{r: r}
	c := &loopCtl{id: id, ctx: context.Background()}
	it, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	seen = lp.settleRules(c, it)
	saved, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.StartRules == nil || seen.StartRules == nil || saved.StartRules.Pending != seen.StartRules.Pending || saved.StartRules.Unread != seen.StartRules.Unread {
		t.Fatalf("the round did not get what was saved: %+v, %+v", seen.StartRules, saved.StartRules)
	}
	return saved
}

func pendingRules() factory.StartRules {
	r := factory.StartRules{AskFirst: []string{"house rule"}, Never: []string{}}
	pipeline.MarkUnread(&r, pipeline.ErrNotHereYet)
	return r
}

// THE REPOSITORY'S RULES ARE READ ONCE IT IS HERE, BEFORE THE FIRST MODEL CALL:
// a start that could not read them because the repository was not on this
// computer yet settles them then, and never tells a person to check syntax.
func TestRulesAreReadAfterTheClone(t *testing.T) {
	t.Run("no file: default rules, nothing waits", func(t *testing.T) {
		none := pipeline.Defaults()
		none.Source, none.From = pipeline.SourceDefaults, factory.FromDefault
		it := afterClone(t, pendingRules(), func(context.Context, factory.Item) (pipeline.File, error) { return none, nil })
		if r := it.StartRules; r.Unread || r.Pending || !r.NoFile || r.UnreadWhy != "" {
			t.Fatalf("%+v", r)
		}
		if pipeline.PlanHold(it.StartRules, nil) != "" {
			t.Fatal("the plan waits for a repository with no file")
		}
	})
	t.Run("a clean file's rules apply", func(t *testing.T) {
		f := pipeline.Defaults()
		f.Source, f.From = pipeline.SourceFile, factory.FromMainBranch
		f.Start.Never = []string{"auth/"}
		f.DefaultBudget = 1
		it := afterClone(t, pendingRules(), func(context.Context, factory.Item) (pipeline.File, error) { return f, nil })
		if r := it.StartRules; r.Unread || r.Pending || r.NoFile || len(r.Never) != 1 {
			t.Fatalf("%+v", r)
		}
		if len(it.StartRules.AskFirst) != 1 {
			t.Fatalf("the factory's own rules went: %+v", it.StartRules)
		}
		if it.Cap != 1 {
			t.Fatalf("the file's lower budget did not apply: %v", it.Cap)
		}
	})
	t.Run("a bad file stays unread, names its line, and its plan waits", func(t *testing.T) {
		it := afterClone(t, pendingRules(), func(context.Context, factory.Item) (pipeline.File, error) {
			return pipeline.Defaults(), errors.New("yaml: line 3: did not find expected key")
		})
		r := it.StartRules
		if !r.Unread || r.Pending || r.UnreadLine != 3 || r.UnreadWhy != pipeline.RepoFile+" line 3: did not find expected key" {
			t.Fatalf("%+v", r)
		}
		if len(r.AskFirst) != 1 {
			t.Fatalf("the factory's own rules went: %+v", r)
		}
		if pipeline.PlanHold(r, nil) == "" {
			t.Fatal("the plan does not wait")
		}
	})
	t.Run("a file whose steps differ from the ones running holds the plan", func(t *testing.T) {
		f := pipeline.Defaults()
		f.Source, f.From = pipeline.SourceFile, factory.FromMainBranch
		f.Pipeline = map[factory.Kind][]string{factory.KindIssue: {"plan", "write"}}
		it := afterClone(t, pendingRules(), func(context.Context, factory.Item) (pipeline.File, error) { return f, nil })
		if r := it.StartRules; !r.Unread || r.Pending || pipeline.PlanHold(r, nil) == "" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("the factory's own rules unread: it stays waiting, and held", func(t *testing.T) {
		it := afterClone(t, pendingRules(), func(context.Context, factory.Item) (pipeline.File, error) {
			return pipeline.Defaults(), pipeline.ErrHouseRulesUnread
		})
		if r := it.StartRules; !r.Unread || !r.Pending {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("a job whose rules are settled is not read again", func(t *testing.T) {
		reads := 0
		it := afterClone(t, factory.StartRules{Never: []string{"auth/"}}, func(context.Context, factory.Item) (pipeline.File, error) {
			reads++
			return pipeline.Defaults(), nil
		})
		if reads != 0 || len(it.StartRules.Never) != 1 {
			t.Fatalf("reads %d, %+v", reads, it.StartRules)
		}
	})
}

// Through the runner: the clone, then the read, then the first model call, and
// that call is handed the settled rules.
func TestARoundReadsTheRulesAfterTheCloneBeforeItsFirstModelCall(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rules := pendingRules()
	id, err := st.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "x", Kind: factory.KindIssue, Cap: factory.DefaultJobBudget, StartRules: &rules, Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true, Until: factory.UntilDone}}})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var order []string
	say := func(what string) { mu.Lock(); defer mu.Unlock(); order = append(order, what) }
	got := make(chan factory.StartRules, 1)
	f := pipeline.Defaults()
	f.Source, f.From = pipeline.SourceFile, factory.FromMainBranch
	f.Start.Never = []string{"auth/"}
	r := New(Options{
		Store:           st,
		Workdir:         func(factory.Item) (string, error) { say("clone"); return t.TempDir(), nil },
		RulesAfterClone: func(context.Context, factory.Item) (pipeline.File, error) { say("read"); return f, nil },
		Exec: map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			say("model")
			got <- *job.Item.StartRules
			return factory.StageResult{Done: true}, nil
		})},
	})
	if err := r.Launch(id); err != nil {
		t.Fatal(err)
	}
	select {
	case seen := <-got:
		if seen.Pending || seen.Unread || len(seen.Never) != 1 {
			t.Fatalf("the round's rules: %+v", seen)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the round never ran")
	}
	for deadline := time.Now().Add(10 * time.Second); r.Running(id) && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) < 3 || order[0] != "clone" || order[1] != "read" || order[2] != "model" {
		t.Fatalf("order: %v", order)
	}
}
