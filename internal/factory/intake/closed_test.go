package intake_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
)

func TestAutomaticIntakeChecksClosedIssues(t *testing.T) {
	for _, policy := range []string{"start:\n  labels: [ready]\n", "start:\n  labels: [ready]\n  ask_first: [changes secrets]\n"} {
		for _, standing := range []string{"closed", "merged", "", "error", "nil"} {
			t.Run(policy+standing, func(t *testing.T) {
				ctx := context.Background()
				st, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				before, err := st.Create(factory.Item{Repo: "owner/repo", Num: 1, Origin: factory.OriginForge, Kind: factory.KindIssue, State: factory.StateNew, Labels: []string{"ready", "size:S"}})
				if err != nil {
					t.Fatal(err)
				}
				f, err := pipeline.Parse(policy)
				if err != nil {
					t.Fatal(err)
				}
				history, err := record.Open(st, record.Options{})
				if err != nil {
					t.Fatal(err)
				}
				before, err = st.Get(before.ID)
				if err != nil {
					t.Fatal(err)
				}

				reads, scouts, launches := 0, 0, 0
				opts := intake.Options{Record: history, Load: func(string) (pipeline.File, error) { return f, nil }, Scout: func(context.Context, int) error { scouts++; return nil }, Launch: func(id int) error {
					launches++
					return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
				}}
				if standing != "nil" {
					opts.Standing = func(ctx context.Context, _ factory.Item) (string, error) {
						reads++
						deadline, ok := ctx.Deadline()
						if !ok || time.Until(deadline) > 5*time.Second {
							t.Error("standing read lacks five second bound")
						}
						if standing == "error" {
							return "", errors.New("unreachable")
						}
						return standing, nil
					}
				}
				in := intake.New(st, opts)
				for range 2 {
					if err := in.Once(ctx); err != nil {
						t.Fatal(err)
					}
				}
				after, err := st.Get(before.ID)
				if err != nil {
					t.Fatal(err)
				}
				events, err := history.Since(ctx, time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				if standing == "closed" || standing == "merged" {
					if scouts != 0 || launches != 0 || !reflect.DeepEqual(before, after) || len(events) != 0 {
						t.Fatalf("closed issue spent or changed: scouts=%d launches=%d unchanged=%v events=%d", scouts, launches, reflect.DeepEqual(before, after), len(events))
					}
					if reads != 1 {
						t.Fatalf("closed issue across two ticks: reads=%d, want 1", reads)
					}
				} else {
					ask := len(f.Start.AskFirst) > 0
					if ask {
						if after.State != factory.StateNeedsYou || launches != 0 {
							t.Fatalf("ask lost: state=%s launches=%d", after.State, launches)
						}
					} else if launches != 1 || scouts != 1 {
						t.Fatalf("start lost: scouts=%d launches=%d", scouts, launches)
					}
					if standing != "nil" && reads != 1 {
						t.Fatalf("decided issue read %d times, want 1", reads)
					}
				}
			})
		}
	}
}

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

func TestClosedIssueCacheSharedByAutomaticPaths(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "owner/repo", Num: 1, Origin: factory.OriginForge, Kind: factory.KindIssue, State: factory.StateNew, Labels: []string{"ready", "size:S"}})
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	standing := func(context.Context, factory.Item) (string, error) { reads++; return "closed", nil }
	opts := intake.Options{Standing: standing, Record: history, Load: func(string) (pipeline.File, error) { return pipeline.Parse("start:\n  labels: [ready]\n") }, Launch: func(int) error { t.Error("closed issue launched"); return nil }}
	if err := intake.New(st, opts).Once(ctx); err != nil {
		t.Fatal(err)
	}
	// A new store handle and reader still share the process's closed answer.
	again, err := store.Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	triage.ReadOne(ctx, again, func(context.Context, string) (string, error) {
		t.Error("closed issue read")
		return "", nil
	}, triage.Options{Standing: standing})
	if reads != 1 {
		t.Fatalf("shared closed answer: reads=%d", reads)
	}
}
