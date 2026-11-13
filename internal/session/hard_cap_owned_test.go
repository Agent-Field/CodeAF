package session

import (
	"context"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/config"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestHardCapStopsConcurrentCrewsAndExplainsIdleChats(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	var agents []*Agent
	var updates []<-chan Event
	var cuts []chan struct{}
	for i := range 3 {
		started, cut := make(chan struct{}), make(chan struct{})
		model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
			if isPlannerCall(messages) {
				return textResponse(`{"add":[{"id":"read-notes","title":"release notes","goal":"read release notes"}]}`), true
			}
			if isNameCall(messages) || isSessionTitleCall(messages) {
				return textResponse("Read notes"), true
			}
			return nil, false
		}, steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(started)
			<-ctx.Done()
			close(cut)
			return nil, ctx.Err()
		}}}
		a, _ := newTestAgent(t, model, func(c *Config) {
			c.DailyBudgetResolved = true
			c.DailyBudgetUSD = .24
			c.SessionFile = filepath.Join(t.TempDir(), fmt.Sprintf("crew-%d.jsonl", i))
		})
		events, leave := a.WatchTaskUpdates()
		defer leave()
		runID, err := a.RunOrchestrate(context.Background(), "read release notes", "", 5)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			snap, _ := a.OrchestrateSnapshot(runID)
			t.Fatalf("crew worker did not start: snapshot=%+v calls=%d", snap, model.requests())
		}
		agents = append(agents, a)
		updates = append(updates, events)
		cuts = append(cuts, cut)
	}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "crossing crew", Calls: 1, USD: .25})
	for i, a := range agents {
		waitCloseRun(t, cuts[i], "crew cancellation")
		deadline := time.After(3 * time.Second)
		chat, work := false, false
		for !chat || !work {
			select {
			case event := <-updates[i]:
				chat = chat || event.Kind == EventNotice && strings.Contains(event.Text, "/budget day")
				work = work || event.Task != nil && event.Task.Stopped && strings.Contains(event.Task.Report, "/budget day")
			case <-deadline:
				t.Fatalf("crew %d missing limit explanation: chat=%v work=%v", i, chat, work)
			}
		}
		found := false
		for _, entry := range a.Transcript() {
			found = found || strings.Contains(entry.Text, "/budget day")
		}
		if !found {
			t.Errorf("idle crew %d lost its persisted stop explanation", i)
		}
		if _, err := a.RunOrchestrate(context.Background(), "new crew", "", 5); err == nil {
			t.Error("stopped owner admitted a crew")
		}
	}
}

func TestHardCapPairsAnInterruptedToolBatch(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, .24); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 3)
	model := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := toolResponse("first", "touch", "{}")
			response.Choices[0].Message.ToolCalls = append(response.Choices[0].Message.ToolCalls, toolResponse("second", "touch", "{}").Choices[0].Message.ToolCalls[0])
			return response, nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			validateToolPairs(t, messages)
			return textResponse("resumed"), nil
		},
	}}
	a, _ := newTestAgent(t, model, func(c *Config) { c.ProfileDir = profile })
	a.tools = append(a.tools, countingTool("touch", started, make(chan struct{})))
	events := mustSubmit(t, a, "work")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("tool batch did not start")
	}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "crossing crew", Calls: 1, USD: .25})
	collect(t, events)
	validateToolPairs(t, a.snapshot())
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "tool batch joins", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	collect(t, mustSubmit(t, a, "resume"))
}
