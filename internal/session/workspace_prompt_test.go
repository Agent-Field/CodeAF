package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cell"
)

const arrivedRules = "ARRIVED-HOUSE-RULE-7f3a: run make check before saying done."

// movedAgent is an agent built while its chat's files are still arriving: the
// workspace is empty, the arrival is open, and the page is the agent's own.
func movedAgent(t *testing.T, completer Completer) (*Agent, string, *cell.WorkspaceArrival) {
	t.Helper()
	workspace := t.TempDir()
	arrival := cell.BeginWorkspaceArrival(workspace, workspace, func() time.Duration { return 5 * time.Second }, nil)
	t.Cleanup(arrival.Complete)
	agent, err := newTestAgentWithConfig(t, Config{Workspace: workspace, Model: "test/model", TaskAudit: true, ApprovalPosture: PostureAllow}, completer)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	agent.jobs.gate = newRenderGate(jobRenderCap)
	return agent, workspace, arrival
}

func systemOfRequest(t *testing.T, completer *scriptedCompleter, i int) string {
	t.Helper()
	completer.mu.Lock()
	defer completer.mu.Unlock()
	if i >= len(completer.seen) {
		t.Fatalf("only %d requests were made, want request %d", len(completer.seen), i+1)
	}
	return messageText(completer.seen[i][0])
}

func drainEvents(events <-chan Event) {
	for range events {
	}
}

// A chat that moved here opens while its files arrive, so the page its agent
// was built with could not read the project's own rules. The first request
// after the files have landed carries them.
func TestTheRequestAfterAMovedChatsFilesArriveCarriesTheirInstructions(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("opened first"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("after the files"), nil },
	}}
	agent, workspace, arrival := movedAgent(t, completer)
	defer agent.Close()

	events, err := agent.Submit(t.Context(), "hello while the files are on their way")
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(events)

	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(arrivedRules+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	arrival.Complete()

	events, err = agent.Submit(t.Context(), "and now?")
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(events)

	if strings.Contains(systemOfRequest(t, completer, 0), arrivedRules) {
		t.Fatal("the request made before the files arrived already carries rules that were not there")
	}
	if !strings.Contains(systemOfRequest(t, completer, 1), arrivedRules) {
		t.Fatal("the first request after the files arrived does not carry the project's AGENTS.md")
	}
}

// The same moment inside one turn: a tool that waits for the files lets the
// next step of that turn, not only the next turn, read the rules.
func TestAStepAfterTheFilesLandMidTurnCarriesTheirInstructions(t *testing.T) {
	entered := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(entered)
			return toolResponse("call_read_rules", "read", `{"path":"notes.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, workspace, arrival := movedAgent(t, completer)
	defer agent.Close()

	events, err := agent.Submit(t.Context(), "read notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { drainEvents(events); close(finished) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the model was never asked")
	}
	// The tool is waiting on the arrival; the files land now.
	for name, body := range map[string]string{"AGENTS.md": arrivedRules + "\n", "notes.txt": "notes\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	arrival.Complete()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn did not finish once the files landed")
	}
	if strings.Contains(systemOfRequest(t, completer, 0), arrivedRules) {
		t.Fatal("the first request already carries rules that had not arrived")
	}
	if !strings.Contains(systemOfRequest(t, completer, 1), arrivedRules) {
		t.Fatal("the step after the files landed does not carry the project's AGENTS.md")
	}
}

// A chat whose files never waited keeps its page byte for byte.
func TestAChatThatNeverWaitedOnFilesKeepsItsPageUntouched(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("one"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("two"), nil },
	}}
	workspace := t.TempDir()
	agent, err := newTestAgentWithConfig(t, Config{Workspace: workspace, Model: "test/model", TaskAudit: true}, completer)
	if err != nil {
		t.Fatal(err)
	}
	agent.jobs.gate = newRenderGate(jobRenderCap)
	defer agent.Close()
	for _, text := range []string{"first", "second"} {
		events, err := agent.Submit(t.Context(), text)
		if err != nil {
			t.Fatal(err)
		}
		drainEvents(events)
	}
	if systemOfRequest(t, completer, 0) != systemOfRequest(t, completer, 1) {
		t.Fatal("a chat that never waited on files had its page rewritten between requests")
	}
}
