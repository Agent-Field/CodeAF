package scout_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/scout"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

var now = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

func floor(t *testing.T) (*store.Store, factory.Item) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew, Title: "retry", Body: "back off",
		Triage: factory.Triage{Size: "L", SizeFrom: factory.SizeFromRead, Rubric: &factory.Rubric{From: factory.RubricFromRead, Scope: factory.Factor{Score: 2}}}})
	if err != nil {
		t.Fatal(err)
	}
	return st, it
}

func counting(n *int, head string) scout.Runner {
	return func(_ context.Context, it factory.Item) (factory.Scout, error) {
		*n++
		return factory.Scout{Body: factory.TextHash(it), Head: head, USD: 0.02, Unknowns: []string{"Which limit?"},
			Rubric: &factory.Rubric{From: factory.RubricFromScout}}, nil
	}
}

func head(sha string) scout.Head {
	return func(context.Context, string) (string, error) { return sha, nil }
}

func TestLookCloserWritesTheScoutAndItsRubricOnTheJob(t *testing.T) {
	st, it := floor(t)
	runs := 0
	got, cached, err := scout.Door{Floor: st, Run: counting(&runs, "aaa"), Head: head("aaa"), Limits: scout.DefaultLimits, Now: func() time.Time { return now }}.Look(context.Background(), it.ID, false)
	if err != nil || cached || runs != 1 {
		t.Fatalf("look = %+v cached %v err %v runs %d", got, cached, err, runs)
	}
	saved, _ := st.Get(it.ID)
	if saved.Scout == nil || !saved.Scout.At.Equal(now) || saved.Triage.Rubric.From != factory.RubricFromScout || saved.Triage.Size != "S" {
		t.Fatalf("job after look = scout %+v triage %+v", saved.Scout, saved.Triage)
	}
	if len(saved.Triage.Questions) != 1 {
		t.Fatalf("unknowns did not become questions: %v", saved.Triage.Questions)
	}
	if l, _ := st.Scouts(now); l.Month != 0.02 || l.Today["acme/api"] != 1 {
		t.Fatalf("ledger = %+v", l)
	}
}

func TestASecondLookAtTheSameIssueAndHeadIsCachedAndFree(t *testing.T) {
	st, it := floor(t)
	runs := 0
	door := scout.Door{Floor: st, Run: counting(&runs, "aaa"), Head: head("aaa"), Limits: scout.DefaultLimits, Now: func() time.Time { return now }}
	_, _, _ = door.Look(context.Background(), it.ID, false)
	if _, cached, err := door.Look(context.Background(), it.ID, false); err != nil || !cached || runs != 1 {
		t.Fatalf("second look cached %v err %v runs %d", cached, err, runs)
	}
	door.Head = head("bbb")
	if _, cached, _ := door.Look(context.Background(), it.ID, false); cached || runs != 2 {
		t.Fatalf("a moved head was served from the cache (runs %d)", runs)
	}
	if _, cached, _ := door.Look(context.Background(), it.ID, true); cached || runs != 3 {
		t.Fatalf("refresh was served from the cache (runs %d)", runs)
	}
}

func TestLookCloserStopsAtItsLimits(t *testing.T) {
	st, it := floor(t)
	runs := 0
	door := scout.Door{Floor: st, Run: counting(&runs, "aaa"), Head: head("aaa"), Limits: scout.Limits{Month: 0.03, PerRepoDay: 5}, Now: func() time.Time { return now }}
	_, _, _ = door.Look(context.Background(), it.ID, true)
	_, _, _ = door.Look(context.Background(), it.ID, true)
	if _, _, err := door.Look(context.Background(), it.ID, true); !errors.Is(err, scout.ErrMonthLimit) || runs != 2 {
		t.Fatalf("over the month: err %v runs %d", err, runs)
	}
	door.Limits = scout.Limits{Month: 10, PerRepoDay: 2}
	if _, _, err := door.Look(context.Background(), it.ID, true); !errors.Is(err, scout.ErrDayLimit) {
		t.Fatalf("over the day: err %v", err)
	}
}
