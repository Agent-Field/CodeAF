package run_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

type costTaskCompleter struct{ calls atomic.Int32 }

func (c *costTaskCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	if c.calls.Add(1) > 1 {
		return nil, errors.New("API error (401): scripted end")
	}
	cost := .05
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "Task work is ready."}}}}},
		Usage:   &ai.Usage{PromptTokens: 100, CompletionTokens: 10, Cost: &cost},
	}, nil
}

func TestCostSplitsActualPaidTaskWorkerCalls(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	stub := filepath.Join(t.TempDir(), "plan-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_PLANDB_BIN", stub)
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "test", "t-root", "Paid task", "Do the work")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	seat := &costTaskCompleter{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run.ChatEngine.Start(ctx, session.RunSpec{
		Store: store, Workspace: t.TempDir(), ProfileDir: t.TempDir(), Conversation: "1111111111111111",
		OneModel: "test/model", Slots: 1, StepsPerTask: 2,
		CompleterFor: func(string) session.Completer { return seat },
	})
	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), Session: "1111111111111111", Model: "test/model", Calls: 1, USD: .01})
	session.FlushUsage()
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, line := range lines {
		total += line.USD
	}
	tree := session.UsageTree(lines, "1111111111111111")
	if total < .06-1e-12 || total > .06+1e-12 || tree.Calls != 2 || tree.Direct != .01 || tree.Children != .05 || tree.Folded() != total {
		t.Fatalf("/cost cannot split actual worker calls: total=%g tree=%+v rows=%+v", total, tree, lines)
	}
}
