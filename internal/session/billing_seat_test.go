package session

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	procexec "github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/telemetry"
	"os"
)

type billingSeat struct {
	procexec.Seat
	mu     sync.Mutex
	calls  []procexec.ModelCall
	sealed []procexec.ModelCall
}

func (s *billingSeat) NoteModelCall(call procexec.ModelCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}
func (s *billingSeat) Settle(_ context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sealed = append(s.sealed, s.calls...)
	s.calls = nil
}

func TestProviderBillingFactsReachTheSeat(t *testing.T) {
	seat := &billingSeat{Seat: procexec.Host}
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	t.Cleanup(func() { StopUsageWriter(ledger) })
	agent, _ := newTestAgent(t, pricedProvider(t, provider.IncludedDoorFact("test", "plan")), func(c *Config) {
		c.Seat = seat
		c.usageLedger = ledger
		c.SessionFile = filepath.Join(t.TempDir(), "transcript.jsonl")
		c.JobBudget = &namedBudgetStub{liveBudgetStub: liveBudgetStub{cap: 5}, job: "o/r#1"}
	})
	collect(t, mustSubmit(t, agent, "hello"))
	agent.reconciled(provider.Reconciled{Found: true, PriceMissing: true, Billed: provider.Billed{Model: "unpriced", PromptTokens: 30, Billing: provider.MeteredDoorFact("test", "metered")}})
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := agent.WaitReceipts(t.Context()); err != nil {
		t.Fatal(err)
	}
	FlushUsage()
	rows, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	seat.mu.Lock()
	defer seat.mu.Unlock()
	notes := append(append([]procexec.ModelCall{}, seat.sealed...), seat.calls...)
	byID := map[string]procexec.ModelCall{}
	for _, call := range notes {
		byID[call.ID] = call
	}
	for _, row := range rows {
		call, ok := byID[row.ID]
		if row.Model == "unpriced" {
			if !ok || call.Billing != "metered" || call.BillingBasis != "door:test/metered" || !call.Unbilled || call.Kind != "" || call.TokensIn != 30 || call.CostMicroUSD != 0 || call.FactoryJob != "o/r#1" {
				t.Fatalf("unpriced call=%+v row=%+v", call, row)
			}
			continue
		}
		if !ok || call.Billing != "included" || call.BillingBasis != "door:test/plan" || call.FactoryJob != "o/r#1" || call.CostMicroUSD != 13000 {
			t.Fatalf("call=%+v row=%+v", call, row)
		}
	}
}

func TestLateMissingReceiptIsSealedBeforeWaitReturns(t *testing.T) {
	before := telemetry.Snapshot().ModelCalls
	seat := &billingSeat{Seat: procexec.Host}
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Seat = seat
		c.usageLedger = path
		c.SessionFile = filepath.Join(t.TempDir(), "transcript.jsonl")
	})
	t.Cleanup(func() { StopUsageWriter(path) })
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	done := agent.pendingReceipt()
	go func() {
		agent.reconciled(provider.Reconciled{Tag: "title", Billed: provider.Billed{Model: "missing", Billing: provider.MeteredDoorFact("test", "metered")}})
		done()
	}()
	if err := agent.WaitReceipts(t.Context()); err != nil {
		t.Fatal(err)
	}
	FlushUsage()
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	seat.mu.Lock()
	defer seat.mu.Unlock()
	if len(seat.calls) != 0 || len(seat.sealed) != 1 {
		t.Fatalf("pending=%+v sealed=%+v", seat.calls, seat.sealed)
	}
	got := seat.sealed[0]
	if got.ID == "" || got.ID != rows[0].ID || got.Kind != procexec.UnbilledMarkerKind || !got.Unbilled || got.Billing != "metered" || got.BillingBasis != "door:test/metered" || got.Role != "title" || rows[0].Billing != got.Billing || got.CostMicroUSD != 0 || got.TokensIn != 0 {
		t.Fatalf("marker=%+v row=%+v", got, rows[0])
	}
	data, err := os.ReadFile(agent.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if books := ReadTranscriptBytes(data).Requests; len(books.Recent) != 0 || books.Latest != 0 {
		t.Fatalf("marker entered chat call list: %+v", books)
	}
	if got := telemetry.Snapshot().ModelCalls; got != before {
		t.Fatalf("marker changed telemetry calls: %d -> %d", before, got)
	}
	if got := agent.Usage(); got.Calls != 0 || got.CostUSD != 0 {
		t.Fatalf("marker changed live calls: %+v", got)
	}
}
