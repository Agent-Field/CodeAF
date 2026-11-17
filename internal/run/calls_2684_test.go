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
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/trace"
)

type callCountingSeat2684 struct {
	calls atomic.Int32
}

func (c *callCountingSeat2684) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	id := calllog.NewID()
	runID := trace.RunFrom(ctx)
	calllog.Append(calllog.Record{
		Time:  time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		ID:    id,
		Run:   runID,
		Phase: calllog.PhaseStart,
		Model: "test/model",
	})
	n := c.calls.Add(1)
	if n == 1 {
		cost := 0.001
		calllog.Append(calllog.Record{
			Time:             time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
			ID:               id,
			Run:              runID,
			Model:            "test/model",
			Status:           200,
			Finish:           "tool_calls",
			PromptTokens:     100,
			CompletionTokens: 20,
			Cost:             cost,
		})
		return &ai.Response{
			Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: []ai.ToolCall{{
					ID:       "call-1",
					Type:     "function",
					Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"true"}`},
				}},
			}}},
			Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, Cost: &cost},
		}, nil
	}
	calllog.Append(calllog.Record{
		Time:   time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		ID:     id,
		Run:    runID,
		Model:  "test/model",
		Status: 401,
		Error:  "API error (401): scripted end",
	})
	return nil, errors.New("API error (401): scripted end")
}

// #2684: A run that makes two model calls through the run engine under the
// door's run id files both calls under that run id in the call log, even when
// the process fallback has been cleared by another run.
func TestRunEngineCountsCallsUnderDoorRunID2684(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(calllog.EnvVar, calllog.OffValue)
	calllog.Open("")
	stub := filepath.Join(t.TempDir(), "plan-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_PLANDB_BIN", stub)

	// Open a second trace run so the process-level fallback is cleared and only
	// an explicit run id on the context can attribute the worker's calls.
	doorCtx := trace.Begin(context.Background())
	_ = trace.Begin(context.Background())
	runID := trace.RunFrom(doorCtx)
	if runID == "" {
		t.Fatal("door minted no run id")
	}

	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "test", "t-root", "Two-call task", "Do the work")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	seat := &callCountingSeat2684{}
	workspace := t.TempDir()
	profileDir := t.TempDir()
	ctx, cancel := context.WithTimeout(trace.WithRun(context.Background(), runID), 20*time.Second)
	defer cancel()

	_, _ = run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: workspace,
		Title:     "Two-call task",
		Brief:     "Do the work",
		Slots:     1,
		Limits:    run.Limits{StepsPerTask: 3},
		Factory: run.CrewFactory(store, workspace, profileDir, run.Seats{
			Work:  "test/model",
			Plan:  "test/model",
			Check: "test/model",
		}, "", true, func(string) session.Completer { return seat }),
	})

	if got := int(seat.calls.Load()); got != 2 {
		t.Fatalf("seat saw %d calls, want 2", got)
	}
	if got := calllog.CallsFor(runID); got != 2 {
		t.Fatalf("calllog.CallsFor(%q) = %d, want 2", runID, got)
	}
}
