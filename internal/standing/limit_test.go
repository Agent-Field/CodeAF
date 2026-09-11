package standing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// spendingRunner comes back having spent usd, cut at its limit.
type spendingRunner struct{ usd float64 }

func (spendingRunner) Probe(context.Context, Item) (string, error) { return "", nil }
func (spendingRunner) Say(context.Context, Item, string) (Outcome, error) {
	return Outcome{}, errors.New("unexpected say")
}
func (r spendingRunner) Run(context.Context, Item, string, string) (Outcome, error) {
	return Outcome{Kind: OutcomeFailed, Text: "the run reached its step or spending limit before it finished", USD: r.usd, Withheld: "at-a-limit"}, nil
}

// A RUN THAT WENT PAST ITS LIMIT IS RECORDED AS HAVING DONE SO. The limit is
// checked between requests, so one request can carry a run past it (validator
// S25a: $0.072 against $0.06); the record keeps the limit it ran under beside
// what it spent, so the overshoot is on the record and not assumed away.
func TestARunThatSpentPastItsLimitSaysSoInItsRecord(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	if _, _, err := store.Revise(made.ID, made.SpecRevision, func(item *Item) error { item.Rails.PerRunUSD = 0.06; return nil }); err != nil {
		t.Fatal(err)
	}
	runner := spendingRunner{usd: 0.072}
	mustTick(t, newTicker(store, runner, now)) // the baseline
	writeFile(t, filepath.Join(workspace, "inbox", "new.md"), "new")
	store.clock = held(now.Add(time.Minute))
	mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	records, err := store.Occurrences(made.ID, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("records %+v %v", records, err)
	}
	done := records[0]
	if done.PerRunUSD != 0.06 || done.USD != 0.072 {
		t.Fatalf("the record lost its limit or its spend: %+v", done)
	}
	if over := done.OverLimit(); over < 0.0119 || over > 0.0121 {
		t.Fatalf("the overshoot reads %v", over)
	}
	if (Occurrence{USD: 0.05, PerRunUSD: 0.06}).OverLimit() != 0 || (Occurrence{USD: 0.05}).OverLimit() != 0 {
		t.Fatal("a run within its limit, or with none, read as over it")
	}
}
