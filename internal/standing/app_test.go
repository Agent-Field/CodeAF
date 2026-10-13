package standing

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func appRoutine(t *testing.T, store *Store, now time.Time) Item {
	t.Helper()
	item := reminder("report each minute", now.Add(time.Hour))
	item.When = When{Kind: WhenEvery, Every: "1m", Words: "each minute"}
	item.Rails = Rails{PerRunUSD: DefaultPerRunUSD, MaxPerDay: 100}
	item.App = &AppOrder{Scope: "this_chat"}
	made, err := store.Propose(item)
	if err != nil {
		t.Fatal(err)
	}
	return made
}

func TestAppOrderRatificationFiveApprovalsSkipPauseAndReset(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := appRoutine(t, store, now)
	runner := &fakeRunner{}
	ticker := newTicker(store, runner, now.Add(time.Hour))
	if pass := mustTick(t, ticker); pass.Fired != 0 || len(runner.said) != 0 {
		t.Fatal("a proposal ran")
	}
	if item.Schema != 5 || SchemaOf(Item{}) != 1 || SchemaOf(Item{Positive: "x"}) != 4 {
		t.Fatal("schema fences changed")
	}
	item, err := store.RatifyApp(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	decide := func(id, verb string) {
		t.Helper()
		var err error
		item, err = store.ChangeApp(item.ID, func(it *Item) error { return DecideAppRun(it, id, verb) })
		if err != nil {
			t.Fatal(err)
		}
	}
	trigger := func() string {
		t.Helper()
		now = now.Add(time.Minute)
		store.clock = held(now)
		ticker.Now = held(now)
		pass := mustTick(t, ticker)
		item = mustGet(t, store, item.ID)
		if pass.Fired != 0 || item.AppWaitingRun() == nil {
			t.Fatalf("trigger ran without approval: %+v %+v", pass, item)
		}
		return item.AppWaitingRun().ID
	}
	first := trigger()
	decide(first, "skip")
	if item.App.ApprovedRuns != 0 {
		t.Fatal("skip earned trust")
	}
	for i := 1; i <= TrustAfter; i++ {
		id := trigger()
		decide(id, "go")
		if item.App.ApprovedRuns != i {
			t.Fatalf("approval %d: %+v", i, item.App)
		}
		if pass := mustTick(t, ticker); pass.Fired != 1 {
			t.Fatalf("approved run: %+v", pass)
		}
		item = mustGet(t, store, item.ID)
		if _, err := store.ChangeApp(item.ID, func(it *Item) error { return DecideAppRun(it, id, "go") }); !errors.Is(err, ErrAppConflict) {
			t.Fatalf("duplicate approval: %v", err)
		}
	}
	if item.AppTrust() != "trusted" {
		t.Fatal(item.AppTrust())
	}
	now = now.Add(time.Minute)
	store.clock = held(now)
	ticker.Now = held(now)
	if pass := mustTick(t, ticker); pass.Fired != 1 {
		t.Fatalf("trusted run: %+v", pass)
	}
	item, err = store.ChangeApp(item.ID, func(it *Item) error { it.Status = StatusPaused; return nil })
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	store.clock = held(now)
	ticker.Now = held(now)
	if pass := mustTick(t, ticker); pass.Fired != 0 {
		t.Fatal("paused order ran")
	}
	item = mustGet(t, store, item.ID)
	if item.AppTrust() != "trusted" || item.App.Skipped != 1 {
		t.Fatalf("pause changed track: %+v", item.App)
	}
	mustTick(t, ticker)
	if mustGet(t, store, item.ID).App.Skipped != 1 {
		t.Fatal("one trigger was counted twice")
	}
	raw, err := os.ReadFile(store.LogPath(item.ID))
	if err != nil || !strings.Contains(string(raw), "Skipped while paused") {
		t.Fatalf("missing skipped line: %s %v", raw, err)
	}
	decide(item.App.Runs[1].ID, "not_right")
	if item.App.ApprovedRuns != 0 || item.AppTrust() != "asks_first" {
		t.Fatal("Not right did not reset trust")
	}
	if _, err := store.ChangeApp(item.ID, func(it *Item) error { return DecideAppRun(it, "missing", "go") }); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAppReminderFiresWithoutTrustAndRetires(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := reminder("remind me", now.Add(time.Minute))
	item.App = &AppOrder{Scope: "not_here"}
	made, err := store.Propose(item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RatifyApp(made.ID); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	ticker := newTicker(store, runner, now.Add(time.Minute))
	if pass := mustTick(t, ticker); pass.Fired != 1 {
		t.Fatal(pass)
	}
	got := mustGet(t, store, made.ID)
	if got.Status != StatusRetired || got.RetiredWhy != "fired" || len(got.App.Runs) != 0 || got.App.ApprovedRuns != 0 {
		t.Fatalf("reminder: %+v", got)
	}
	if pass := mustTick(t, ticker); pass.Fired != 0 {
		t.Fatal("reminder fired twice")
	}
}

func TestAppApprovedRunKeepsExactEvidenceAndCannotReplayAfterStop(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made := appRoutine(t, store, now)
	made, err := store.RatifyApp(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	made, err = store.ChangeApp(made.ID, func(it *Item) error {
		it.When = When{Kind: WhenProbe, ProbeEvery: time.Minute, Probe: Probe{Command: "read"}}
		it.Does.Say = "{{evidence}}"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{evidence: "first"}
	ticker := newTicker(store, runner, now.Add(time.Minute))
	ticker.Sentinel, _ = answers(true, true)
	if _, err = ticker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	made = mustGet(t, store, made.ID)
	run := made.AppWaitingRun()
	if run == nil || run.Evidence != "first" {
		t.Fatal("trigger evidence was not saved")
	}
	_, err = store.ChangeApp(made.ID, func(it *Item) error { return DecideAppRun(it, run.ID, "go") })
	if err != nil {
		t.Fatal(err)
	}
	runner.evidence = "second"
	if pass := mustTick(t, ticker); pass.Fired != 1 {
		t.Fatal(pass)
	}
	if len(runner.said) != 1 || runner.said[0] != "first" || runner.probeSeen != 1 {
		t.Fatalf("approval reran probe: %+v", runner)
	}
	_, err = store.ChangeApp(made.ID, func(it *Item) error { it.Status = StatusRetired; it.RetiredWhy = "stopped by you"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if pass := mustTick(t, ticker); pass.Fired != 0 {
		t.Fatal("stopped order ran")
	}
}

func mustGet(t *testing.T, store *Store, id string) Item {
	t.Helper()
	item, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestAppPausedProbeKeepsBilledChecksInsideDailyRail(t *testing.T) {
	for name, verdict := range map[string]SentinelReading{"ready": VerdictYes, "unknown": VerdictUnknown} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
			store := openStore(t, now)
			item := appRoutine(t, store, now)
			item, err := store.RatifyApp(item.ID)
			if err != nil {
				t.Fatal(err)
			}
			item, err = store.ChangeApp(item.ID, func(it *Item) error {
				it.Status = StatusPaused
				it.When = When{Kind: WhenProbe, ProbeEvery: time.Minute, Probe: Probe{Command: "read"}}
				it.NextDue = now
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{evidence: "CI run 12 failed"}
			ticker := newTicker(store, runner, now)
			ticker.DailyRailUSD = 0.02
			ticker.SentinelVerdict = func(context.Context, Judgment) (SentinelReading, string, float64, error) {
				return verdict, "CI failed", 0.02, nil
			}
			if pass := mustTick(t, ticker); pass.Errors != 0 || pass.Fired != 0 {
				t.Fatal(pass)
			}
			item = mustGet(t, store, item.ID)
			if item.SpentUSD != 0.02 || len(runner.said) != 0 {
				t.Fatalf("paused billing: %+v", item)
			}
			if verdict == VerdictYes && item.App.Skipped != 1 {
				t.Fatal("positive skipped trigger missing")
			}
			ticker.Now = held(now.Add(time.Minute))
			mustTick(t, ticker)
			if runner.probeSeen != 1 {
				t.Fatal("paused probe spent past the daily rail")
			}
		})
	}
}

func TestAppScheduledWorkRequiresApprovalAtItsDueTime(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := reminder("look at CI tomorrow", now.Add(time.Hour))
	item.Does = Action{Kind: ActionTask, Brief: "Look at CI failures"}
	item.App = &AppOrder{Scope: "this_chat"}
	made, err := store.Propose(item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RatifyApp(made.ID); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	ticker := newTicker(store, runner, now.Add(time.Hour))
	if pass := mustTick(t, ticker); pass.Fired != 0 || len(runner.runDirs) != 0 {
		t.Fatal("scheduled work ran without its run approval")
	}
	got := mustGet(t, store, made.ID)
	if got.AppWaitingRun() == nil {
		t.Fatal("scheduled work has no run card")
	}
	_, err = store.ChangeApp(made.ID, func(it *Item) error { return DecideAppRun(it, got.AppWaitingRun().ID, "go") })
	if err != nil {
		t.Fatal(err)
	}
	if pass := mustTick(t, ticker); pass.Fired != 1 || len(runner.runDirs) != 1 {
		t.Fatal(pass)
	}
	if got = mustGet(t, store, made.ID); got.Status != StatusRetired {
		t.Fatal("one-shot work did not end")
	}
}
