package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/store"
)

func appLedgerFixture(t *testing.T, path string, rows ...UsageLine) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, row := range rows {
		if err := json.NewEncoder(f).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppSpendingCountsChildrenAndResetsOnlyDailyScopes(t *testing.T) {
	profile := t.TempDir()
	ledger := filepath.Join(profile, "v3", UsageLedgerName)
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	appLedgerFixture(t, ledger, UsageLine{At: now, Session: "child", Root: "a", Task: "1", USD: 5})
	if err := AppSpendingBlock(profile, ledger, "a", "", now); err == nil || !strings.Contains(err.Error(), "agent cap reached") {
		t.Fatal(err)
	}
	if err := AppSpendingBlock(profile, ledger, "b", "1", now); err != nil {
		t.Fatal(err)
	}
	if err := AppSpendingBlock(profile, ledger, "a", "", now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if err := AppSpendingBlock(profile, ledger, "a", "1", now.AddDate(0, 0, 1)); err == nil || !strings.Contains(err.Error(), "task cap reached") {
		t.Fatal(err)
	}
	if err := config.RaiseAppCap(profile, "task", config.AppTaskCapID("a", "1"), 2, 8, false, now); err != nil {
		t.Fatal(err)
	}
	if err := AppSpendingBlock(profile, ledger, "a", "1", now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestAppAgentCapStopsTheRealNextCallAndProducesNeedsPerson(t *testing.T) {
	profile := t.TempDir()
	ledger := filepath.Join(profile, "v3", UsageLedgerName)
	c := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			t.Fatal("capped call reached provider")
			return nil, nil
		},
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.ProfileDir = profile; cfg.usageLedger = ledger })
	if err := a.SetAppBudget(5); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	id := a.appAgentIDLocked()
	a.mu.Unlock()
	appLedgerFixture(t, ledger, UsageLine{At: time.Now(), Session: "child", Root: id, USD: 5})
	events := collect(t, mustSubmit(t, a, "Keep working"))
	if len(events) != 1 || !errors.Is(events[0].Err, ErrSpendRail) || c.requests() != 0 || !a.NeedsPerson() {
		t.Fatalf("events %v calls %d waiting %v", kinds(events), c.requests(), a.NeedsPerson())
	}
	if err := config.RaiseAppCap(profile, "agent", id, 5, 8, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a.NeedsPerson() {
		t.Fatal("raised allowance left the agent waiting")
	}
	if err := a.appSpendingBlock(); err != nil {
		t.Fatal(err)
	}
}

func TestAppDayTemporaryRaiseIsUsedByTurnAdmission(t *testing.T) {
	profile := t.TempDir()
	ledger := filepath.Join(profile, "v3", UsageLedgerName)
	c := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Allowed"), nil }}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.ProfileDir = profile; cfg.usageLedger = ledger })
	if err := a.SetAppBudget(5); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteDailyBudgetUSD(profile, 20); err != nil {
		t.Fatal(err)
	}
	appLedgerFixture(t, ledger, UsageLine{At: time.Now(), Session: "other", USD: 20})
	if err := config.RaiseAppCap(profile, "day", "", 20, 30, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, a, "Keep working"))
	if c.requests() != 1 {
		t.Fatalf("calls %d", c.requests())
	}
}

func TestAppTaskGuardReadsRaisesWithoutLosingItsTally(t *testing.T) {
	profile := t.TempDir()
	a := &Agent{config: Config{ProfileDir: profile, AppSpending: true}}
	guard := &SpendGuard{Task: &SpendTask{spent: 2}, Day: NewSpendDay(0)}
	a.bindAppGuard(guard, "1")
	if _, err := guard.before(context.Background(), "unknown", nil, nil); err == nil {
		t.Fatal("task at its cap started")
	}
	if err := config.RaiseAppCap(profile, "task", config.AppTaskCapID("unfiled", "1"), 2, 3, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.before(context.Background(), "unknown", nil, nil); err != nil {
		t.Fatal(err)
	}
	if guard.Task.Total() != 2 {
		t.Fatal("raise reset measured spending")
	}
}

func TestMemoryAdditionSwitchKeepsExplicitSavesAndExistingReads(t *testing.T) {
	profile := t.TempDir()
	brain, err := store.Open(filepath.Join(profile, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.Memory = brain; cfg.ProfileDir = profile; cfg.RolesSource = nil })
	if err := config.ApplyAppSettings(profile, map[string]json.RawMessage{"memory.agents_may_add": json.RawMessage("false")}, ""); err != nil {
		t.Fatal(err)
	}
	if tools := a.memoryTools(); len(tools) != 0 {
		t.Fatal("model can still add memory")
	}
	if _, err := a.Remember("User explicitly says to save this"); err != nil {
		t.Fatal(err)
	}
	if rows, err := a.Memories("explicitly"); err != nil || len(rows) != 1 {
		t.Fatalf("explicit memory %v %v", rows, err)
	}
	a.learnFromTurn("Invent another fact", "Keep it")
	if rows, err := brain.ListMemories(nil, 0); err != nil || len(rows) != 1 {
		t.Fatalf("automatic write %v %v", rows, err)
	}
}

func TestAppHelpersAndRestoredTaskGuardsShareLiveRaises(t *testing.T) {
	profile := t.TempDir()
	a := &Agent{config: Config{ProfileDir: profile, AppSpending: true}}
	r := &TaskCrewRecord{Version: 1, Routed: true, Guard: &crewGuardRecord{TaskCap: 2, TaskSpent: 2}, Original: map[crewroute.Seat]string{}}
	for _, seat := range crewroute.Seats {
		r.Original[seat] = "unknown"
		r.Decision.Crew = append(r.Decision.Crew, crewroute.Pick{Seat: seat, Send: "unknown"})
	}
	crew, err := a.restoreTaskCrew(1, r)
	if err != nil {
		t.Fatal(err)
	}
	helper := a.helperGuard(crew)
	if helper.Limits == nil || helper.Task != crew.guard.Task {
		t.Fatal("helper lost its restored task scope")
	}
	if _, err := helper.before(context.Background(), "unknown", nil, nil); err == nil {
		t.Fatal("helper escaped task cap")
	}
	if err := config.RaiseAppCap(profile, "task", config.AppTaskCapID("unfiled", "1"), 2, 3, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := helper.before(context.Background(), "unknown", nil, nil); err != nil {
		t.Fatal(err)
	}
	if crew.guard.Task.Total() != 2 {
		t.Fatal("restore or raise erased the tally")
	}
}

func TestAppBindingRefreshesTaskGuardsRestoredBeforeAttachment(t *testing.T) {
	profile := t.TempDir()
	a := &Agent{config: Config{ProfileDir: profile}}
	guard := &SpendGuard{Day: NewSpendDay(0), Task: &SpendTask{spent: 2}, TaskCap: 2}
	a.crews.put(1, &taskCrew{guard: guard})
	if err := a.SetAppBudget(5); err != nil {
		t.Fatal(err)
	}
	if guard.liveLimits() == nil {
		t.Fatal("app attachment left the restored guard frozen")
	}
	if err := config.RaiseAppCap(profile, "task", config.AppTaskCapID("unfiled", "1"), 2, 3, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.before(context.Background(), "unknown", nil, nil); err != nil {
		t.Fatal(err)
	}
	if guard.Task.Total() != 2 {
		t.Fatal("attachment reset task spending")
	}
}

func TestAppSavedTaskBoundaryAndSnapshotReadTheRaisedLimit(t *testing.T) {
	a, node, _ := appNodeFixture(t, false)
	profile := t.TempDir()
	a.config.ProfileDir = profile
	a.config.taskCapUSD = 2
	a.config.usageLedger = filepath.Join(profile, "v3", UsageLedgerName)
	if err := a.SetAppBudget(5); err != nil {
		t.Fatal(err)
	}
	node.graph.mu.Lock()
	node.cost = 2
	node.graph.mu.Unlock()
	a.mu.Lock()
	owner := a.appAgentIDLocked()
	a.mu.Unlock()
	if err := config.RaiseAppCap(profile, "task", config.AppTaskCapID(owner, "1"), 2, 3, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	rows, err := a.AppTasks()
	if err != nil || len(rows) != 1 || rows[0].Cap != 3 {
		t.Fatalf("task snapshot %v %v", rows, err)
	}
	node.graph.mu.Lock()
	node.cost = 3
	node.graph.mu.Unlock()
	_, _, err = a.completeWithRetry(context.Background(), nil, a.Model(), "", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "$3 limit") {
		t.Fatalf("raised task boundary: %v", err)
	}
}
