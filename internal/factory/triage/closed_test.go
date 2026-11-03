package triage_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
)

func TestArrivalReadChecksClosedIssues(t *testing.T) {
	for _, door := range []string{"tick"} {
		for _, standing := range []string{"closed", "merged", "", "error", "nil", "local", "unnumbered"} {
			t.Run(door+"/"+standing, func(t *testing.T) {
				st, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				item := factory.Item{Repo: "owner/repo", Num: 1, Origin: factory.OriginForge, Kind: factory.KindIssue, State: factory.StateNew}
				if standing == "local" {
					item.Origin = factory.OriginChat
				}
				if standing == "unnumbered" {
					item.Num = 0
				}
				before, err := st.Create(item)
				if err != nil {
					t.Fatal(err)
				}
				before, err = st.Get(before.ID)
				if err != nil {
					t.Fatal(err)
				}

				reads, calls := 0, 0
				opts := triage.Options{}
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
						if standing == "local" || standing == "unnumbered" {
							t.Error("non-forge or unnumbered row checked")
						}
						return standing, nil
					}
				}
				call := func(context.Context, string) (string, error) { calls++; return `{"read":"Fix the issue"}`, nil }
				for range 2 {
					triage.ReadOne(context.Background(), st, call, opts)
				}
				after, err := st.Get(before.ID)
				if err != nil {
					t.Fatal(err)
				}
				if standing == "closed" || standing == "merged" {
					if calls != 0 || !reflect.DeepEqual(before, after) {
						t.Fatalf("closed issue read or changed: calls=%d unchanged=%v", calls, reflect.DeepEqual(before, after))
					}
					if reads != 1 {
						t.Fatalf("closed issue across two ticks: reads=%d, want 1", reads)
					}
				} else if calls != 1 {
					t.Fatalf("open or unavailable issue: calls=%d, want 1", calls)
				}
			})
		}
	}
}

// A person's Read press reads: they asked, and a button that did nothing would lie.
func TestAPersonsReadPressReadsAClosedIssue(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.Create(factory.Item{Repo: "acme/api", Product: "acme", Num: 1, Kind: factory.KindIssue, Origin: factory.OriginForge, Title: "t", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	call := func(context.Context, string) (string, error) { calls++; return `{"read":"Fix the issue"}`, nil }
	opts := triage.Options{Standing: func(context.Context, factory.Item) (string, error) { return "closed", nil }}
	got := triage.ReadThese(context.Background(), st, call, []int{row.ID}, opts)
	if calls != 1 || len(got.Read) != 1 {
		t.Fatalf("press on a closed issue: calls=%d result=%+v", calls, got)
	}
}
