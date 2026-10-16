package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/service/servicetest"
)

// keepCall is one scripted bash call asking to keep a command running.
func keepCall(id, command string, keep map[string]any) step {
	raw, _ := json.Marshal(map[string]any{"command": command, "keep_running": keep})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "bash", string(raw)), nil
	}
}

// keepWorld is a supervisor over a machine with no processes, shared by the
// conversations of one test the way one codeaf shares it.
func keepWorld(t *testing.T) (*service.Supervisor, *servicetest.Runtime) {
	t.Helper()
	rt := servicetest.New()
	supervisor, err := service.Open(service.Options{Dir: t.TempDir(), Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(supervisor.Close)
	return supervisor, rt
}

// keepAgent is a conversation somebody is in, owning its folder "owner".
func keepAgent(t *testing.T, completer Completer, supervisor *service.Supervisor, owner string) *Agent {
	t.Helper()
	folder := filepath.Join(t.TempDir(), owner)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Services = supervisor
		config.SessionFile = filepath.Join(folder, "transcript.jsonl")
	})
	return agent
}

// drainAnsweringKeep drains a turn, answering every keep card with key through
// the one question door, and every other approval with a one-time yes.
func drainAnsweringKeep(t *testing.T, agent *Agent, events <-chan Event, key string, seen func([]Question)) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			switch {
			case event.Kind == EventConsentRequest:
				agent.ResolveConsentRemember(event.ID, true, ConsentOnce)
			case event.Kind == EventKeepRunning && event.Keep != nil && event.Keep.Decided == "":
				if seen != nil {
					seen(agent.OpenQuestions())
				}
				if err := agent.ResolveQuestion(Answer{Kind: QuestionKeep, ID: event.Keep.ID, Key: key, DecidedBy: DecidedByPerson}); err != nil {
					t.Errorf("answering the card: %v", err)
				}
			}
		case <-deadline:
			t.Fatal("the turn never ended")
		}
	}
}

func toolResultText(t *testing.T, agent *Agent, callID string) string {
	t.Helper()
	agent.mu.Lock()
	messages := append([]ai.Message(nil), agent.messages...)
	agent.mu.Unlock()
	for _, message := range messages {
		if message.Role == "tool" && message.ToolCallID == callID {
			return messageText(message)
		}
	}
	t.Fatalf("no result for %s", callID)
	return ""
}

// TestAllowKeepsItRunningUnderTheProposedPolicy is the board's first ask: a
// real card on the question door, three answers, a receipt, a kept service.
func TestAllowKeepsItRunningUnderTheProposedPolicy(t *testing.T) {
	supervisor, rt := keepWorld(t)
	completer := &scriptedCompleter{steps: []step{
		keepCall("k1", "npm run dev", map[string]any{"name": "web dev server", "health": map[string]any{"port": 5173}, "restart": "always"}),
		finalText("it is running"),
	}}
	agent := keepAgent(t, completer, supervisor, "api-docs")
	events, err := agent.Submit(context.Background(), "start the web dev server and keep it up")
	if err != nil {
		t.Fatal(err)
	}
	var open []Question
	collected := drainAnsweringKeep(t, agent, events, KeepAllowKey, func(q []Question) { open = q })
	if len(open) != 1 || open[0].Kind != QuestionKeep || len(open[0].Options) != 3 || !open[0].Blocking.Turn {
		t.Fatalf("the card is one open question with three answers: %+v", open)
	}
	if card := open[0].Keep; card == nil || card.Command != "npm run dev" || card.Health.Port != 5173 || card.Policy != service.PolicyAlways || card.Name != "web dev server" || card.Dir == "" {
		t.Fatalf("the card does not carry its proposal: %+v", card)
	}
	if rt.Starts() != 1 {
		t.Fatalf("started %d", rt.Starts())
	}
	rows := supervisor.List("api-docs")
	if len(rows) != 1 || rows[0].Policy != service.PolicyAlways || !rows[0].Running || rows[0].Name != "web dev server" ||
		rows[0].From.Label != "start the web dev server and keep it up" || rows[0].From.CallID != "k1" {
		t.Fatalf("kept: %+v", rows)
	}
	var receipt *KeepNotice
	for _, event := range collected {
		if event.Kind == EventKeepRunning && event.Keep.Decided != "" {
			receipt = event.Keep
		}
	}
	if receipt == nil || receipt.Decided != KeepAllowed || receipt.Service == nil || receipt.Service.ID != rows[0].ID {
		t.Fatalf("the receipt: %+v", receipt)
	}
	if text := toolResultText(t, agent, "k1"); !strings.Contains(text, "Kept running as "+rows[0].ID) {
		t.Fatalf("the model was told %q", text)
	}
	if len(agent.OpenQuestions()) != 0 {
		t.Fatal("an answered card is still open")
	}
}

// TestTheSameCommandIsAskedOnceAndThePersonsPolicyStands.
func TestTheSameCommandIsAskedOnceAndThePersonsPolicyStands(t *testing.T) {
	supervisor, rt := keepWorld(t)
	keep := map[string]any{"health": map[string]any{"port": 5173}, "restart": "always"}
	completer := &scriptedCompleter{steps: []step{
		keepCall("k1", "npm run dev", keep), finalText("kept"),
		keepCall("k2", "npm run dev", keep), finalText("again"),
	}}
	agent := keepAgent(t, completer, supervisor, "api-docs")
	events, _ := agent.Submit(context.Background(), "keep it up")
	drainAnsweringKeep(t, agent, events, KeepAllowNeverKey, nil)
	rows := supervisor.List("api-docs")
	if len(rows) != 1 || rows[0].Policy != service.PolicyNever {
		t.Fatalf("allow, never restart: %+v", rows)
	}
	if _, err := supervisor.Stop("api-docs", rows[0].ID); err != nil {
		t.Fatal(err)
	}
	events, _ = agent.Submit(context.Background(), "start it again")
	collected := drainAnsweringKeep(t, agent, events, KeepDenyKey, nil)
	if _, asked := firstOfKind(collected, EventKeepRunning); asked {
		t.Fatal("the same command in the same folder was asked about twice")
	}
	rows = supervisor.List("api-docs")
	if rt.Starts() != 2 || len(rows) != 1 || !rows[0].Running || rows[0].Policy != service.PolicyNever {
		t.Fatalf("the agent's always replaced the person's never: %+v", rows)
	}
}

// TestDenyRunsAnOrdinaryJobThatEndsWithTheTurn.
func TestDenyRunsAnOrdinaryJobThatEndsWithTheTurn(t *testing.T) {
	supervisor, rt := keepWorld(t)
	completer := &scriptedCompleter{steps: []step{
		keepCall("k1", "sleep 300", map[string]any{"health": map[string]any{"cmd": "true"}}),
		finalText("ok"),
	}}
	agent := keepAgent(t, completer, supervisor, "api-docs")
	events, _ := agent.Submit(context.Background(), "run it")
	collected := drainAnsweringKeep(t, agent, events, KeepDenyKey, nil)
	if rt.Starts() != 0 || len(supervisor.List("api-docs")) != 0 {
		t.Fatal("a denied command was kept")
	}
	if _, ok := supervisor.Approved(service.Spec{Command: "sleep 300", Dir: agent.config.Workspace, Health: service.Health{Cmd: "true"}}); ok {
		t.Fatal("a no was banked as a yes")
	}
	var job int
	for _, event := range collected {
		if event.Kind == EventKeepRunning && event.Keep.Decided == KeepDenied {
			job = event.Keep.Job
		}
	}
	if job == 0 {
		t.Fatal("the deny receipt names no job")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		found := agent.jobs.find(job)
		if found != nil && !found.running() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the denied job outlived its turn")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if text := toolResultText(t, agent, "k1"); !strings.Contains(text, "stops when this turn ends") {
		t.Fatalf("the model was told %q", text)
	}
}

// TestWithNoSupervisorThereIsNoKeepRunning: absent, not broken.
func TestWithNoSupervisorThereIsNoKeepRunning(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		keepCall("k1", "sleep 300", map[string]any{"health": map[string]any{"port": 1}}),
		finalText("ok"),
	}}
	agent := keepAgent(t, completer, nil, "api-docs")
	for _, tool := range agent.belt() {
		if tool.Name == "bash" && strings.Contains(string(tool.Schema), "keep_running") {
			t.Fatal("bash offers keep_running with nothing behind it")
		}
	}
	events, _ := agent.Submit(context.Background(), "run it")
	drainAnsweringKeep(t, agent, events, KeepAllowKey, nil)
	if text := toolResultText(t, agent, "k1"); !strings.Contains(text, "not available here") {
		t.Fatalf("the model was told %q", text)
	}
	if len(agent.jobs.all()) != 0 {
		t.Fatal("something was started")
	}
}

// TestOneConversationNeverListsAnothersServices.
func TestOneConversationNeverListsAnothersServices(t *testing.T) {
	supervisor, _ := keepWorld(t)
	if _, err := supervisor.Keep("other-agent", service.Spec{Command: "make serve", Dir: t.TempDir(), Health: service.Health{Port: 8080}}, service.PolicyNever, service.Provenance{}); err != nil {
		t.Fatal(err)
	}
	agent := keepAgent(t, &scriptedCompleter{}, supervisor, "api-docs")
	if listed := agent.keptRunningList(); listed != "" {
		t.Fatalf("another agent's service is listed here: %q", listed)
	}
}

// TestEventOrdinalsAfterAutomationUpdateDoNotMove pins the persisted event
// numbers: a summary's start and failure keep the two ordinals after
// EventAutomationUpdate, and the keep-running ask the third.
func TestEventOrdinalsAfterAutomationUpdateDoNotMove(t *testing.T) {
	if EventSummaryStarted != EventAutomationUpdate+1 || EventSummaryFailed != EventAutomationUpdate+2 || EventKeepRunning != EventAutomationUpdate+3 {
		t.Fatalf("ordinals moved: started %d failed %d keep %d after %d", EventSummaryStarted, EventSummaryFailed, EventKeepRunning, EventAutomationUpdate)
	}
}
