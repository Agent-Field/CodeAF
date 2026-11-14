package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A TEAM SERVER FOLLOWS A GO-AHEAD BY ITSELF: with no app attached and no
// GitHub grant to read through, the served factory's poll loop runs the
// go-ahead pass on every tick and starts the job a policy covers, attributed
// to its allowance. A pass whose launch was refused is tried again on the next
// tick rather than counted as a failed GitHub read.
func TestAServedFactoryStartsAGoAheadsJobWithNoAppAttached(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	history, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "owner/repo", Kind: factory.KindIssue, Origin: factory.OriginChat, State: factory.StateNew, Title: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ChangePolicy("ws", func(p *factory.GroupPolicy) error {
		p.Owner = "alice"
		p.Allowances = []factory.Allowance{{ID: "ga_team", Jobs: []int{it.ID}, CapUSD: 10, EndsAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	refused := true
	started := make(chan int, 4)
	in := intake.New(st, intake.Options{Record: history, Launch: func(id int) error {
		if refused {
			refused = false
			return errors.New("no worker yet")
		}
		started <- id
		return st.Update(id, func(it *factory.Item) error { it.State = factory.StateQueued; return nil })
	}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ticks := make(chan struct{}, 8)
	t.Cleanup(startFactoryServerPoll(ctx, st, filepath.Join(t.TempDir(), "tokens.json"), &githubLimit{}, func(ctx context.Context) error {
		defer func() { ticks <- struct{}{} }()
		return in.GoAheads(ctx)
	}))

	// The first tick's launch is refused; the job says so and is not started.
	select {
	case <-ticks:
	case <-time.After(10 * time.Second):
		t.Fatal("the served factory's poll never ran the go-ahead pass")
	}
	if got, _ := st.Get(it.ID); got.PolicyRun != "" || got.Blocked == nil || got.Blocked.Allowance != "ga_team" {
		t.Fatalf("after a refused launch the job reads %+v", got)
	}
	// The next tick, woken as any store write wakes the poll, starts it.
	if err := st.NudgePoll(); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-started:
		if id != it.ID {
			t.Fatalf("started job %d, want %d", id, it.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the served factory never started the job its go-ahead covers")
	}
	if got, _ := st.Get(it.ID); got.PolicyRun != "ga_team" || got.PolicyGroup != "ws" || got.Starter != "alice" {
		t.Fatalf("the started job reads policy_run %q, group %q, starter %q", got.PolicyRun, got.PolicyGroup, got.Starter)
	}
}
