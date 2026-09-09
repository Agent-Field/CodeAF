package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type simpleLoopCompleter struct {
	mu        sync.Mutex
	rounds    int
	sideCalls int
	answer    func(context.Context, []ai.Message, int) (*ai.Response, error)
}

func (c *simpleLoopCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	if len(request.Tools) == 0 {
		if isNameCall(messages) {
			return textResponse(""), nil
		}
		c.mu.Lock()
		c.sideCalls++
		c.mu.Unlock()
		return textResponse("No further work."), nil
	}
	c.mu.Lock()
	c.rounds++
	round := c.rounds
	c.mu.Unlock()
	return c.answer(ctx, messages, round)
}
func (c *simpleLoopCompleter) FallbackModels(string) []string { return nil }
func simpleLoopAgent(t *testing.T, c Completer) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, c, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierMastermind): "test/reader"})
	})
}

// A substantial request can be answered directly. Neither its size nor a
// text-only response authorizes another model to classify or certify the answer.
func TestSimpleLoopFinalAnswerDoesNotBuyApproval(t *testing.T) {
	c := &simpleLoopCompleter{answer: func(context.Context, []ai.Message, int) (*ai.Response, error) {
		return textResponse("The comparison is complete: use the documented process."), nil
	}}
	agent, _ := simpleLoopAgent(t, c)
	collect(t, mustSubmit(t, agent, "Compare the available approaches, explain the tradeoffs in detail, and give me a practical recommendation with its limitations."))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rounds != 1 || c.sideCalls != 0 {
		t.Fatalf("answer used %d working calls and %d extra decision calls", c.rounds, c.sideCalls)
	}
	if !strings.Contains(messageContentText(lastMessage(agent)), "comparison is complete") {
		t.Fatal("the direct answer was replaced")
	}
}

// Work can cross the old five-write and forty-round thresholds without losing
// its owner or paying for a new task. Every preceding result stays on the next
// request, and the final answer can end the turn without a separate reader.
func TestSimpleLoopManyWritesStayWithTheCurrentAgent(t *testing.T) {
	const writes = 45
	c := &simpleLoopCompleter{answer: func(_ context.Context, messages []ai.Message, round int) (*ai.Response, error) {
		if round > 1 {
			found := false
			for _, m := range messages {
				if m.Role == "tool" && m.ToolCallID == fmt.Sprintf("write-%d", round-1) {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("previous result missing on round %d", round)
			}
		}
		if round > writes {
			return textResponse("All requested records are written."), nil
		}
		return toolResponse(fmt.Sprintf("write-%d", round), "write", fmt.Sprintf(`{"path":"record-%d.txt","content":"record %d"}`, round, round)), nil
	}}
	agent, workspace := simpleLoopAgent(t, c)
	events := collect(t, mustSubmit(t, agent, "Write each record to its own file and complete the full set here, preserving all of the supplied content."))
	for _, e := range events {
		if e.Kind == EventTaskProposal || e.Kind == EventError {
			t.Fatalf("ordinary work left its loop: %+v", e)
		}
	}
	for i := 1; i <= writes; i++ {
		body, err := os.ReadFile(filepath.Join(workspace, fmt.Sprintf("record-%d.txt", i)))
		if err != nil || string(body) != fmt.Sprintf("record %d", i) {
			t.Fatalf("record %d: %q %v", i, body, err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rounds != writes+1 || c.sideCalls != 0 {
		t.Fatalf("work used %d calls and %d decision calls", c.rounds, c.sideCalls)
	}
}

// Quiet useful work is ordinary work. The loop may offer concise progress
// guidance in its prompt, but visible prose must never become a precondition for
// running a tool or another paid completion inserted to satisfy the harness.
func TestSimpleLoopRunsLongQuietWorkWithoutAProgressProseGate(t *testing.T) {
	const quietRounds = 18
	c := &simpleLoopCompleter{answer: func(_ context.Context, messages []ai.Message, round int) (*ai.Response, error) {
		for _, message := range messages {
			text := messageContentText(message)
			if (message.Role == "user" && strings.HasPrefix(text, "[silent]")) ||
				(message.Role == "tool" && strings.HasPrefix(text, "[held]")) {
				return nil, fmt.Errorf("round %d received a synthetic progress demand: %q", round, text)
			}
		}
		for completed := 1; completed < round && completed <= quietRounds; completed++ {
			found := false
			for _, message := range messages {
				if message.Role == "tool" && message.ToolCallID == fmt.Sprintf("inspect-%d", completed) {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("round %d lacks result inspect-%d", round, completed)
			}
		}
		switch {
		case round <= quietRounds:
			return toolResponse(fmt.Sprintf("inspect-%d", round), "inspect", fmt.Sprintf(`{"item":%d}`, round)), nil
		case round == quietRounds+1:
			return toolResponseWithText("write-summary", "write", `{"path":"summary.txt","content":"all evidence reviewed"}`, "I have finished the review and am writing the result."), nil
		default:
			for _, message := range messages {
				if message.Role == "tool" && message.ToolCallID == "write-summary" {
					return textResponse("The review and summary are complete."), nil
				}
			}
			return nil, fmt.Errorf("final request lacks the write result")
		}
	}}
	agent, workspace := simpleLoopAgent(t, c)
	agent.tools = append(agent.tools, freshTool("inspect"))
	events := collect(t, mustSubmit(t, agent, "Inspect every supplied item, write the complete summary, and finish here."))
	for _, event := range events {
		if event.Kind == EventNudge || (event.Kind == EventToolFailed && event.HarnessMade) {
			t.Fatalf("quiet useful work was interrupted by the harness: %+v", event)
		}
	}
	body, err := os.ReadFile(filepath.Join(workspace, "summary.txt"))
	if err != nil || string(body) != "all evidence reviewed" {
		t.Fatalf("summary: %q %v", body, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rounds != quietRounds+2 || c.sideCalls != 0 {
		t.Fatalf("work used %d calls and %d side calls, want %d and 0", c.rounds, c.sideCalls, quietRounds+2)
	}
}

// Removing automatic handoffs must not swallow a note queued while a request
// is in flight. The next ordinary model request receives it with the tool result.
func TestSimpleLoopCarriesQueuedResultsIntoTheNextRequest(t *testing.T) {
	var agent *Agent
	c := &simpleLoopCompleter{answer: func(_ context.Context, messages []ai.Message, round int) (*ai.Response, error) {
		if round == 1 {
			agent.enqueueSteering("The worker found the requested evidence: alpha.")
			return toolResponse("inspect", "ls", `{"path":"."}`), nil
		}
		note, result := false, false
		for _, m := range messages {
			note = note || strings.Contains(messageContentText(m), "worker found the requested evidence: alpha")
			result = result || (m.Role == "tool" && m.ToolCallID == "inspect")
		}
		if !note || !result {
			return nil, fmt.Errorf("next request lacks note=%v or result=%v", note, result)
		}
		return textResponse("The evidence is alpha."), nil
	}}
	agent, _ = simpleLoopAgent(t, c)
	collect(t, mustSubmit(t, agent, "Inspect the available evidence and incorporate the worker's findings in your answer."))
	if got := messageContentText(lastMessage(agent)); !strings.Contains(got, "evidence is alpha") {
		t.Fatalf("queued result was not used: %s", got)
	}
}

// The caller's context remains the hard boundary even when it would otherwise
// keep issuing tools. Removing a delegation threshold does not remove cancellation.
func TestSimpleLoopExternalCancellationStillStopsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &simpleLoopCompleter{answer: func(_ context.Context, _ []ai.Message, round int) (*ai.Response, error) {
		if round == 5 {
			cancel()
			return nil, context.Canceled
		}
		return toolResponse(fmt.Sprintf("inspect-%d", round), "ls", fmt.Sprintf(`{"path":".","limit":%d}`, round)), nil
	}}
	agent, _ := simpleLoopAgent(t, c)
	events, err := agent.Submit(ctx, "Inspect the available records and keep working through the outstanding material.")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rounds != 5 {
		t.Fatalf("external cancellation left %d calls running", c.rounds)
	}
}

// Exhausting repetition advice must not blind the observer to later progress.
// A real write makes room for the corrective note from a different bad call.
func TestSimpleLoopObservesProgressAfterItsAdviceWasExhausted(t *testing.T) {
	c := &simpleLoopCompleter{answer: func(_ context.Context, messages []ai.Message, round int) (*ai.Response, error) {
		if round <= 9 {
			name := []string{"alpha", "beta", "gamma"}[(round-1)/3]
			return toolResponseWithText(fmt.Sprintf("repeat-%d", round), name, `{"path":"a"}`, "Checking the observation."), nil
		}
		if round == 10 {
			return toolResponseWithText("progress", "write", `{"path":"progress.txt","content":"saved"}`, "Saving the new result."), nil
		}
		if round <= 12 {
			return toolResponseWithText(fmt.Sprintf("invalid-%d", round), "tasks", `{"limit":10.5}`, "Checking the task list."), nil
		}
		for _, message := range messages {
			text := messageContentText(message)
			if strings.Contains(text, "[stuck]") && strings.Contains(text, "Send the corrected call") && strings.Contains(text, "limit") {
				return textResponse("The argument correction was received."), nil
			}
		}
		return textResponse("The new argument correction was missing."), nil
	}}
	agent, workspace := simpleLoopAgent(t, c)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		agent.tools = append(agent.tools, freshTool(name))
	}
	events := collect(t, mustSubmit(t, agent, "Inspect the observations, save the new result, and resolve any malformed arguments."))
	if data, err := os.ReadFile(filepath.Join(workspace, "progress.txt")); err != nil || string(data) != "saved" {
		t.Fatalf("progress never happened: %q %v", data, err)
	}
	if got := messageContentText(lastMessage(agent)); got != "The argument correction was received." {
		t.Fatalf("observer stayed exhausted after material progress: %s", got)
	}
	if got := len(nudgeEvents(events)); got != loopNudgeCeiling+1 {
		t.Fatalf("got %d advice events, want the bounded original advice plus one after progress", got)
	}
}
