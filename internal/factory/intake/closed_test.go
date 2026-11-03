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
