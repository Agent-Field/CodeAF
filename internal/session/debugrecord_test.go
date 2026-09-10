package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/trace"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// recordingRun turns the debug record on for ONE run. The state root is a
// disposable folder so a test cannot write into whoever ran it.
func recordingRun(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("AFORGE_HOME", t.TempDir())
	ctx := trace.Begin(context.Background())
	if trace.EnableRun(ctx) == "" {
		t.Fatal("EnableRun did not take this run")
	}
	return ctx
}

func TestAToolCallIsWrittenToTheDebugRecord(t *testing.T) {
	ctx := recordingRun(t)
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	path := filepath.Join(workspace, "hello.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := agent.executeTool(ctx, agent.newEpisode(), nil,
		withdrawnCall("c1", "read", `{"path":"hello.txt"}`), "")
	if result.isError {
		t.Fatalf("read failed: %s", result.text)
	}

	event := onlyToolEvent(t, ctx)
	if event["tool"] != "read" {
		t.Errorf("tool = %v, want read", event["tool"])
	}
	if event["call"] != "c1" {
		t.Errorf("call = %v, want c1", event["call"])
	}
	if event["status"] != "ok" {
		t.Errorf("status = %v, want ok", event["status"])
	}
	if !strings.Contains(fmtString(event["result"]), "hi") {
		t.Errorf("result = %v, want the file's contents", event["result"])
	}
}

func TestAFailedToolCallIsRecordedAsFailed(t *testing.T) {
	ctx := recordingRun(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	result := agent.executeTool(ctx, agent.newEpisode(), nil,
		withdrawnCall("c2", "transmogrify", `{}`), "")
	if !result.isError {
		t.Fatalf("an unknown tool succeeded: %q", result.text)
	}
	event := onlyToolEvent(t, ctx)
	if event["tool"] != "transmogrify" {
		t.Errorf("tool = %v, want transmogrify", event["tool"])
	}
	if event["status"] != "failed" {
		t.Errorf("status = %v, want failed", event["status"])
	}
	if event["refused_by"] != nil {
		t.Errorf("a miss is not a refusal: refused_by = %v", event["refused_by"])
	}
}

func TestARefusedToolCallNamesWhoSaidNo(t *testing.T) {
	ctx := recordingRun(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{
			Default: approval.ActionAllow,
			Tools:   map[string]approval.Action{"write": approval.ActionDeny},
		}
	})
	result := agent.executeTool(ctx, agent.newEpisode(), nil,
		withdrawnCall("c3", "write", `{"path":"a.md","content":"x"}`), "")
	if !result.isError {
		t.Fatalf("a denied write succeeded: %q", result.text)
	}
	if result.refusedBy != "approval" {
		t.Fatalf("refusedBy = %q, want approval", result.refusedBy)
	}

	event := onlyToolEvent(t, ctx)
	if event["status"] != "refused" {
		t.Errorf("status = %v, want refused", event["status"])
	}
	if event["refused_by"] != "approval" {
		t.Errorf("refused_by = %v, want approval", event["refused_by"])
	}
	if !strings.Contains(fmtString(event["reason"]), "denied by approval rule") {
		t.Errorf("reason = %v, want the gate's own wording", event["reason"])
	}
}

func TestAToolCallWithTheRecordOffWritesNothing(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	ctx := trace.Begin(context.Background())
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	_ = agent.executeTool(ctx, agent.newEpisode(), nil,
		withdrawnCall("c4", "transmogrify", `{}`), "")
	folder := trace.Dir(trace.RunFrom(ctx))
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatalf("the record was off and still wrote %s (%v)", folder, err)
	}
}

func TestAnEffortStampIsADecisionInTheDebugRecord(t *testing.T) {
	ctx := recordingRun(t)
	recordEffort(ctx, "sim/model", effort.High)
	recordEffort(ctx, "sim/model", effort.None)

	event := onlyEvent(t, ctx)
	if event["kind"] != "decision" || event["decision"] != "effort" {
		t.Fatalf("event = %v, want an effort decision", event)
	}
	if event["choice"] != "high" {
		t.Errorf("choice = %v, want high", event["choice"])
	}
	if event["subject"] != "sim/model" {
		t.Errorf("subject = %v, want the model", event["subject"])
	}
}

func onlyToolEvent(t *testing.T, ctx context.Context) map[string]any {
	t.Helper()
	event := onlyEvent(t, ctx)
	if event["kind"] != "tool" {
		t.Fatalf("event = %v, want a tool event", event)
	}
	return event
}

func onlyEvent(t *testing.T, ctx context.Context) map[string]any {
	t.Helper()
	events := allEvents(t, ctx)
	if len(events) != 1 {
		t.Fatalf("got %d events, want one", len(events))
	}
	return events[0]
}

func allEvents(t *testing.T, ctx context.Context) []map[string]any {
	t.Helper()
	path := filepath.Join(trace.Dir(trace.RunFrom(ctx)), trace.EventsFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the debug record wrote no events file: %v", err)
	}
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("an events line is not JSON: %v\n%s", err, line)
		}
		events = append(events, event)
	}
	return events
}

func fmtString(value any) string {
	s, _ := value.(string)
	return s
}
