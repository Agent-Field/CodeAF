package syncsetup

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	factorystore "github.com/Agent-Field/codeaf/internal/factory/store"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Take must preserve stated billing facts as well as the saved call's dollars.
func TestTakePreservesUsageBillingFacts(t *testing.T) {
	for _, facts := range []bool{false, true} {
		name := "legacy"
		if facts {
			name = "facts"
		}
		t.Run(name, func(t *testing.T) { testTakeBillingFacts(t, facts) })
	}
}

func testTakeBillingFacts(t *testing.T, facts bool) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	t.Setenv(home.EnvVar, h.homeA)
	pathA := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(pathA) })
	a := h.openA()
	want := []session.UsageLine{
		{ID: "priced-call", Model: "m", Session: h.cell.ID, Calls: 1, Input: 100, Output: 10, USD: .001,
			Billing: "metered", BillingBasis: "door:test/metered", FactoryJob: "owner/repo#17"},
		{ID: "unpriced-call", Model: "m", Session: h.cell.ID, Calls: 1, Input: 30, Unbilled: true,
			Billing: "metered", BillingBasis: "door:test/metered", FactoryJob: "owner/repo#17"},
	}
	if facts {
		want = append(want, session.UsageLine{ID: "missing-receipt", Session: h.cell.ID, Model: "m", Kind: executor.UnbilledMarkerKind, Unbilled: true, FactoryJob: "owner/repo#17"})
	}
	if !facts {
		for i := range want {
			want[i].Billing = ""
			want[i].BillingBasis = ""
			want[i].FactoryJob = ""
			want[i].Unbilled = false
		}
	}
	for _, row := range want {
		session.RecordUsage(pathA, row)
		executor.NoteModelCall(a.seat, executor.ModelCall{
			ID: row.ID, Kind: row.Kind, Model: row.Model, TokensIn: row.Input, TokensOut: row.Output,
			CostMicroUSD: int64(row.USD * 1e6),
			Billing:      row.Billing, BillingBasis: row.BillingBasis, Unbilled: row.Unbilled, FactoryJob: row.FactoryJob,
		})
	}
	if !session.FlushUsage() {
		t.Fatal("sender ledger did not flush")
	}
	before, err := session.ReadUsage(pathA, time.Time{})
	if err != nil || len(before) != len(want) {
		t.Fatalf("sender rows=%d err=%v, want %d", len(before), err, len(want))
	}
	for i, row := range before {
		if row.ID != want[i].ID || row.Billing != want[i].Billing || row.BillingBasis != want[i].BillingBasis ||
			row.Unbilled != want[i].Unbilled || row.FactoryJob != want[i].FactoryJob {
			t.Fatalf("sender did not retain fixture billing facts on row %d", i)
		}
	}
	a.mustSay("scripted calls finished")
	executor.Settle(t.Context(), a.seat)
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Setenv(home.EnvVar, h.homeB)
	pathB := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(pathB) })
	floor, err := factorystore.Open(filepath.Join(h.homeB, "v3", "factory"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := floor.Create(factory.Item{Repo: "owner/repo", Num: 17, SpentBefore: .25, ChatSpent: .001})
	if err != nil {
		t.Fatal(err)
	}
	lifetime := factory.JobLifetimeSpent(item)
	if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	if !session.FlushUsage() {
		t.Fatal("receiver ledger did not flush")
	}
	after, err := session.ReadUsage(pathB, time.Time{})
	if err != nil || len(after) != len(want) {
		t.Fatalf("receiver rows=%d err=%v, want %d", len(after), err, len(want))
	}
	byID := make(map[string]session.UsageLine, len(after))
	for _, row := range after {
		byID[row.ID] = row
	}
	for _, row := range before {
		got, ok := byID[row.ID]
		if !ok || got.USD != row.USD || got.Input != row.Input || got.Output != row.Output {
			t.Fatalf("receiver changed call identity or figures for %q", row.ID)
		}
		t.Logf("%s: sender/receiver USD=%.6f/%.6f", row.ID, row.USD, got.USD)
		if got.Billing != row.Billing || got.BillingBasis != row.BillingBasis || got.Unbilled != row.Unbilled || got.FactoryJob != row.FactoryJob || got.Kind != row.Kind {
			t.Errorf("%s: billing=%q -> %q, basis=%q -> %q, unbilled=%t -> %t, factory_job=%q -> %q",
				row.ID, row.Billing, got.Billing, row.BillingBasis, got.BillingBasis,
				row.Unbilled, got.Unbilled, row.FactoryJob, got.FactoryJob)
		}
	}
	unbilled := 0
	if facts {
		unbilled = 2
	}
	page, err := session.QueryUsageLedger(pathB, session.UsageQuery{}, "", 10, session.DefaultUsageLedgerBudget)
	if err != nil || page.Totals.USD != .001 || page.Totals.Calls != 2 || page.Totals.Unbilled != unbilled {
		t.Fatalf("taken totals=%+v err=%v", page.Totals, err)
	}
	saved, err := floor.Get(item.ID)
	if err != nil || factory.JobLifetimeSpent(saved) != lifetime || factory.JobSpent(saved) != .001 {
		t.Fatalf("take changed saved job money: %+v err=%v", saved, err)
	}
	// Refreshing an already imported cell must not charge its calls again.
	if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	session.FlushUsage()
	again, err := session.QueryUsageLedger(pathB, session.UsageQuery{}, "", 10, session.DefaultUsageLedgerBudget)
	if err != nil || again.Totals != page.Totals {
		t.Fatalf("repeat take changed totals: %+v err=%v", again.Totals, err)
	}

}

func TestTakeMarkerKeepsLegacyTranscriptSpend(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	t.Setenv(home.EnvVar, h.homeA)
	a := h.openA()
	appendTo(t, transcriptOf(h.cell), `{"type":"usage","timestamp":"2026-09-29T10:00:01Z","usage":{"model":"m","calls":2,"input":30,"output":10,"costUsd":0.75}}`+"\n")
	executor.NoteModelCall(a.seat, executor.ModelCall{ID: "missing", Kind: executor.UnbilledMarkerKind, Model: "m", Unbilled: true})
	a.mustSay("legacy calls and a missing receipt")
	executor.Settle(t.Context(), a.seat)
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeB)
	path := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(path) })
	for range 2 {
		if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
			t.Fatal(err)
		}
		if !session.FlushUsage() {
			t.Fatal("flush")
		}
		page, err := session.QueryUsageLedger(path, session.UsageQuery{}, "", 10, session.DefaultUsageLedgerBudget)
		if err != nil || page.Totals.USD != .75 || page.Totals.Calls != 2 || page.Totals.Unbilled != 1 {
			t.Fatalf("marker hid legacy spending: %+v err=%v", page.Totals, err)
		}
	}
}
