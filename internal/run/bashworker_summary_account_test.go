package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

type summaryAccountSeat struct{ summaryCalls int }

func (s *summaryAccountSeat) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	cost, in, out, text := 0.01, 10, 5, "The log was read."
	if provider.RoleFrom(ctx) == lane.RoleAuxiliary {
		s.summaryCalls++
		cost, in, out = 0.03, 100, 20
		text = "The person supplied a series of old logs. The assistant read each log and reported that it had been read."
	}
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}}}}, Usage: &ai.Usage{PromptTokens: in, CompletionTokens: out, Cost: &cost}}, nil
}

// A real compaction summary banks separately from turn events. Final worker
// accounting must include it even when no turn ends after the manual compact.
func TestWorkerFinalAccountIncludesARealAuxiliarySummary(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	stub := filepath.Join(root, "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_PLANDB_BIN", stub)
	store, err := plandb.Open(filepath.Join(root, "plan.db"), "account", "root", "Read logs", "Read the logs")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seat := &summaryAccountSeat{}
	agent, err := session.NewBeltWorker(session.Config{Workspace: root, Model: "test/model", ContextWindow: 65_536}, seat, store.Task(store.RootID()), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	for i := 0; i < 8; i++ {
		events, err := agent.Submit(t.Context(), strings.Repeat("a line from the old log ", 500))
		if err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event.Kind == session.EventError {
				t.Fatal(event.Err)
			}
		}
	}
	before := agent.Usage()
	if err := agent.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	if seat.summaryCalls != 1 {
		t.Fatalf("summary calls=%d, want one real compaction call", seat.summaryCalls)
	}
	var receipts workerReceipts
	final := settledWorkerUsage(agent, &receipts)
	if final.Input-before.Input != 100 || final.Output-before.Output != 20 || final.CostUSD-before.CostUSD < 0.029999 || final.CostUSD-before.CostUSD > 0.030001 {
		t.Fatalf("summary lost from final account: before=%+v final=%+v", before, final)
	}
}

// Settlement has a finite endpoint even if a provider never retires its receipt.
func TestWorkerReceiptSettlementIsBoundedAndCompletionIsIdempotent(t *testing.T) {
	var receipts workerReceipts
	done := receipts.owe()
	receipts.wait(0)
	done()
	done()
	receipts.wait(0)
}
