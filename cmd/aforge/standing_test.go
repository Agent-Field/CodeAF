package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestBudgetSlashForms(t *testing.T) {
	t.Run("display", func(t *testing.T) {
		commander, graph := openBudgetCommander(t, 20)
		if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: 3.4}); err != nil {
			t.Fatal(err)
		}
		got, err := commander.Budget(nil)
		if err != nil || got != "spent $3.40 of $20 · resets midnight" {
			t.Fatalf("/budget = %q err=%v", got, err)
		}
	})

	t.Run("raise today", func(t *testing.T) {
		commander, graph := openBudgetCommander(t, 20)
		got, err := commander.Budget([]string{"50"})
		if err != nil || got != "today's budget → $50 · resets midnight" {
			t.Fatalf("/budget 50 = %q err=%v", got, err)
		}
		rail, err := graph.DailyRailToday(20)
		if err != nil || rail.Ceiling != 50 || rail.Raised != 30 || rail.Unlimited {
			t.Fatalf("raised rail = %+v err=%v", rail, err)
		}
		assertRailEvent(t, graph, store.RailAdjustment{
			Amount: 30, Origin: "slash:/budget",
		})
	})

	t.Run("persist default", func(t *testing.T) {
		commander, _ := openBudgetCommander(t, 20)
		t.Setenv("AFORGE_DAILY_BUDGET", "")
		got, err := commander.Budget([]string{"default", "35"})
		if err != nil || got != "default daily budget → $35" {
			t.Fatalf("/budget default 35 = %q err=%v", got, err)
		}
		persisted, err := config.DailyBudgetUSDAt(commander.ProfileDir())
		if err != nil || persisted != 35 || commander.DailyBudgetUSD() != 35 {
			t.Fatalf("persisted budget = %v runtime=%v err=%v", persisted,
				commander.DailyBudgetUSD(), err)
		}
	})

	t.Run("unlimited today", func(t *testing.T) {
		commander, graph := openBudgetCommander(t, 20)
		got, err := commander.Budget([]string{"unlimited", "today"})
		if err != nil || got != "today's budget → unlimited · resets midnight" {
			t.Fatalf("/budget unlimited today = %q err=%v", got, err)
		}
		rail, err := graph.DailyRailToday(20)
		if err != nil || !rail.Unlimited {
			t.Fatalf("unlimited rail = %+v err=%v", rail, err)
		}
		assertRailEvent(t, graph, store.RailAdjustment{
			Origin: "slash:/budget unlimited today", Unlimited: true,
		})
	})
}

func TestStandingSlashListsOnlyActiveCharters(t *testing.T) {
	commander, graph := openBudgetCommander(t, 20)
	spec := store.CharterSpec{
		Invariant: "Every morning send the release digest.",
		Watch: store.CharterWatch{
			Kind: store.WatchCron, Cadence: "every morning", Schedule: "0 9 * * *",
		},
		Sentinel: "Is the digest due?", Action: "Send the release digest.",
		Rails: store.CharterSpecRails{
			EstimatedCostUSD: 0.05, MaxPerDay: 1,
			MaxPerDayJustification: "one scheduled delivery", Expiry: "never",
		},
	}
	if _, err := graph.DraftCharter("draft-only", "standing", 0, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.DraftCharter("release-digest", "standing", 0, spec); err != nil {
		t.Fatal(err)
	}
	if err := graph.SetCharterStatus("release-digest", store.CharterActive, store.Ratification{
		Origin: store.OriginUser, SessionID: "standing", Evidence: "yes, stand this up",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := commander.Standing()
	if err != nil {
		t.Fatal(err)
	}
	if got != "release-digest · every morning · Every morning send the release digest." ||
		strings.Contains(got, "draft-only") {
		t.Fatalf("/standing = %q", got)
	}
}

func openBudgetCommander(t *testing.T, daily float64, session ...string) (*chatCommander, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	graph, err := store.Open(filepath.Join(dir, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	options := command.Options{
		Settings: config.Config{DailyBudgetUSD: daily, ProfileDir: dir}, Store: graph,
		AttachSession: func(string) error { return nil },
	}
	if len(session) > 0 {
		options.SessionID = session[0]
	}
	return command.New(options), graph
}

func assertRailEvent(t *testing.T, graph *store.Store, want store.RailAdjustment) {
	t.Helper()
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind != store.EventRailRaised {
			continue
		}
		var got store.RailAdjustment
		if err := json.Unmarshal(event.Payload, &got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("rail event = %+v, want %+v", got, want)
		}
		return
	}
	t.Fatal("rail raise was not journaled")
}
