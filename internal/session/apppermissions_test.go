package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestAppPermissionSavedRulesAndFloorsReachTheToolGate(t *testing.T) {
	profile := t.TempDir()
	policy := &approval.Policy{Default: approval.ActionPrompt}
	agent, runs := bashAgent(t, policy, "printf '*.go'", "printf '*.go'", "rm -rf /")
	agent.config.DeterministicNames = true
	agent.config.ProfileDir = profile
	_, err := config.SaveAppPermissionRule(profile, agent.id, "bash", json.RawMessage(`{"command":"printf '*.go'"}`), "exact", "agent", "ask", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	questionEvents, stop := agent.WatchQuestions()
	defer stop()
	var asked []Event
	drainAnswering(t, mustSubmit(t, agent, "work"), func(ev Event) { asked = append(asked, ev); agent.ResolveConsent(ev.ID, false) })
	if len(asked) != 1 || len(runs) != 2 {
		t.Fatalf("asked %d runs %d", len(asked), len(runs))
	}
	automatic := 0
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for automatic < 2 {
		select {
		case ev := <-questionEvents:
			if ev.Kind == EventQuestionAnswered && ev.Answer != nil && ev.Answer.DecidedBy == DecidedByDial {
				automatic++
			}
		case <-timer.C:
			t.Fatalf("automatic receipts %d", automatic)
		}
	}
}
func TestAppPermissionDontAskAndMemoCannotLiftFloors(t *testing.T) {
	agent, runs := bashAgent(t, &approval.Policy{Default: approval.ActionAllow}, "rm -rf .turbo", "printf done")
	agent.EnableAppPermissions()
	agent.config.ProfileDir = t.TempDir()
	agent.rememberConsent("bash", true)
	asked := 0
	drainAnswering(t, mustSubmit(t, agent, "work"), func(ev Event) { asked++; agent.ResolveConsent(ev.ID, false) })
	if asked != 1 || len(runs) != 1 {
		t.Fatalf("floor bypass: %d asks, %d runs", asked, len(runs))
	}
}
func TestAppTaskPermissionUsesParentQuestionWithoutBlockingChat(t *testing.T) {
	parent, _ := bashAgent(t, &approval.Policy{Default: approval.ActionAllow})
	parent.config.DeterministicNames = true
	parent.config.ProfileDir = t.TempDir()
	child, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.InTask = true
		c.appApprovalOwner = parent
		c.appApprovalTask = "19"
		c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	events, stop := parent.WatchQuestions()
	defer stop()
	done := make(chan bool, 1)
	call := ai.ToolCall{ID: "child-call"}
	call.Function.Name = "bash"
	call.Function.Arguments = `{"command":"rm -rf /"}`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, allowed := child.approve(ctx, newEventHub(), call); done <- allowed }()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	var q Question
	select {
	case ev := <-events:
		if ev.Question == nil {
			t.Fatal("no parent question")
		}
		q = *ev.Question
	case <-timeout.C:
		t.Fatal("task permission was refused instead of asking")
	}
	if q.Blocking.Turn || len(q.Blocking.Tasks) != 1 || q.Blocking.Tasks[0] != "19" || q.PermissionCommand != "rm -rf /" || q.Batch != "task/19" {
		t.Fatalf("task question %+v", q)
	}
	main := mustSubmit(t, parent, "Keep talking while the task waits")
	drain(t, main)
	if err := parent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "1", DecidedBy: DecidedByPerson}); err != nil {
		t.Fatal(err)
	}
	select {
	case allowed := <-done:
		if !allowed {
			t.Fatal("answer did not release the child")
		}
	case <-timeout.C:
		t.Fatal("task remained blocked")
	}
}

func TestAppPermissionLosingAnExternalAnswerRaceIsRefused(t *testing.T) {
	agent, runs := bashAgent(t, &approval.Policy{Default: approval.ActionPrompt}, "printf done")
	agent.config.DeterministicNames = true
	agent.config.ProfileDir = t.TempDir()
	questions, stop := agent.WatchQuestions()
	defer stop()
	events := mustSubmit(t, agent, "work")
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	var q Question
	select {
	case ev := <-questions:
		if ev.Question == nil {
			t.Fatal("question missing")
		}
		q = *ev.Question
	case <-timeout.C:
		t.Fatal("permission missing")
	}
	if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "1", From: "another window"}); err != nil {
		t.Fatal(err)
	}
	if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "3", From: "app"}); err == nil {
		t.Fatal("app's losing answer race reported success")
	}
	drain(t, events)
	if len(runs) != 1 {
		t.Fatal("the first answer did not own the call")
	}
}
