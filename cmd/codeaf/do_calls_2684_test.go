package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// callLoggingCompleter2684 wraps a scripted belt seat with the same calllog
// start/end pairs the provider adapter writes on every outbound request,
// reading the run id off the request context via trace.RunFrom(ctx).
type callLoggingCompleter2684 struct {
	model string
	inner session.Completer
}

func (c callLoggingCompleter2684) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	id := calllog.NewID()
	runID := trace.RunFrom(ctx)
	calllog.Append(calllog.Record{
		Time:  time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		ID:    id,
		Run:   runID,
		Phase: calllog.PhaseStart,
		Model: c.model,
	})
	resp, err := c.inner.CompleteWithMessages(ctx, messages, options...)
	end := calllog.Record{
		Time:   time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		ID:     id,
		Run:    runID,
		Model:  c.model,
		Status: 200,
	}
	if resp != nil && resp.Usage != nil {
		end.PromptTokens = resp.Usage.PromptTokens
		end.CompletionTokens = resp.Usage.CompletionTokens
		if resp.Usage.Cost != nil {
			end.Cost = *resp.Usage.Cost
		}
	}
	if err != nil {
		end.Status = 500
		end.Error = calllog.ClipError(err.Error())
	}
	calllog.Append(end)
	return resp, err
}

// #2684: `codeaf do --json` reports a non-zero, exact `calls` count that equals
// the number of `usage.jsonl` rows for the run and the number of calls in
// `codeaf logs --json --run <id>`, whether or not the call log file is on.
func TestDoJSONReportsExactCallCount2684(t *testing.T) {
	home := beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)

	logPath := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(calllog.EnvVar, logPath)
	calllog.Open("")
	t.Cleanup(func() {
		calllog.Close()
		os.Setenv(calllog.EnvVar, calllog.OffValue)
		calllog.Open("")
	})

	// Mint the door's run id the way runDo does, and also open a second trace
	// run so the process fallback is cleared: the run id must travel on the
	// context from the do door into the run engine's workers.
	traced := openDebugRecord("do", "", workspace)
	_ = trace.Begin(context.Background())
	runID := trace.RunFrom(traced)
	if runID == "" {
		t.Fatal("openDebugRecord minted no run id")
	}

	seat := finishingSeat(0.001)
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		run:       runID,
		task:      "write out.txt and say what you did",
		workspace: workspace,
		asJSON:    true,
		timeout:   time.Minute,
		slots:     bound(1),
		stdout:    &stdout,
		stderr:    &stderr,
		newBeltCompleter: func(model string) session.Completer {
			return callLoggingCompleter2684{model: model, inner: seat}
		},
	})
	if err != nil {
		t.Fatalf("doErrand failed: %v\nstderr:\n%s", err, stderr.String())
	}
	calllog.Close()

	fields := doEnvelopeFields(t, stdout.String())
	if got := fields["run"]; got != runID {
		t.Fatalf("envelope run = %v, want %q", got, runID)
	}
	callsFloat, ok := fields["calls"].(float64)
	if !ok || int(callsFloat) == 0 {
		t.Fatalf("envelope calls = %v, want non-zero call count:\n%s", fields["calls"], stdout.String())
	}
	gotCalls := int(callsFloat)

	// The count must equal the number of usage.jsonl rows written by the run.
	usageLines, err := session.ReadUsage(filepath.Join(home, "v3", "usage.jsonl"), time.Time{})
	if err != nil {
		t.Fatalf("read usage.jsonl: %v", err)
	}
	if gotCalls != len(usageLines) {
		t.Fatalf("envelope calls = %d, usage.jsonl rows = %d", gotCalls, len(usageLines))
	}

	// And it must equal the completed calls in `codeaf logs --json --run <id>`.
	var logsJSON strings.Builder
	if err := runLogsWith([]string{"--json", "--run", runID}, &logsJSON, logPath, time.Now); err != nil {
		t.Fatalf("runLogsWith --json --run %s: %v", runID, err)
	}
	logged, err := decodeCallLog(strings.NewReader(logsJSON.String()))
	if err != nil {
		t.Fatalf("decodeCallLog: %v", err)
	}
	endRows := 0
	for _, row := range logged {
		if row.record.Phase == "" {
			endRows++
		}
	}
	if gotCalls != endRows {
		t.Fatalf("envelope calls = %d, logs --json --run %s end rows = %d", gotCalls, runID, endRows)
	}

	// And `calls` still counts when CODEAF_CALL_LOG=off.
	t.Setenv(calllog.EnvVar, calllog.OffValue)
	calllog.Open("")
	offRunID := trace.NewRunID()
	offSeat := finishingSeat(0.001)
	stdout.Reset()
	stderr.Reset()
	if err := doErrand(doRequest{
		run:       offRunID,
		task:      "write out2.txt and say what you did",
		workspace: beltRepoWorkspace(t),
		asJSON:    true,
		timeout:   time.Minute,
		slots:     bound(1),
		stdout:    &stdout,
		stderr:    &stderr,
		newBeltCompleter: func(model string) session.Completer {
			return callLoggingCompleter2684{model: model, inner: offSeat}
		},
	}); err != nil {
		t.Fatalf("doErrand with call log off failed: %v\nstderr:\n%s", err, stderr.String())
	}
	offFields := doEnvelopeFields(t, stdout.String())
	if got := int(offFields["calls"].(float64)); got != gotCalls {
		t.Fatalf("with CODEAF_CALL_LOG=off, envelope calls = %d, want %d", got, gotCalls)
	}
}
