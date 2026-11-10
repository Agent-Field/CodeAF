package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	taskrun "github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

type costWorkerSeat struct{ calls atomic.Int32 }

func (c *costWorkerSeat) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	if c.calls.Add(1) > 1 {
		return nil, errors.New("API error (401): scripted end")
	}
	cost := .05
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "Task work is ready."}}}}},
		Usage:   &ai.Usage{PromptTokens: 100, CompletionTokens: 10, Cost: &cost},
	}, nil
}

func TestCostShowsConversationAndTasksAfterActualWorkerSpend(t *testing.T) {
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
	worker := taskrun.NewBashWorker(store, t.TempDir(), "test/model", &costWorkerSeat{}, taskrun.WithConversation(treeConversation))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = worker.Run(ctx, *store.Task(store.RootID()))
	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), Session: treeConversation, Model: "test/model", Calls: 1, USD: .01})
	session.FlushUsage()
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	a := spendTreeLab(t, .06, lines)
	a.file = filepath.Join(filepath.Dir(a.file), ".cell", "transcript.jsonl")
	text := a.costText()
	for _, want := range []string{"spend", dollars(.06), "conversation", dollars(.01), "tasks", dollars(.05)} {
		if !strings.Contains(text, want) {
			t.Fatalf("/cost omitted %q:\n%s", want, text)
		}
	}
	if a.tree.Calls != 2 || a.tree.Direct != .01 || a.tree.Children != .05 || a.tree.Folded() < .06-1e-12 || a.tree.Folded() > .06+1e-12 {
		t.Fatalf("split does not add up to spend with each call once: %+v", a.tree)
	}
}
