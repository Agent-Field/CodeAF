package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func discussionWait(t *testing.T, events <-chan Event, matches func(Event) bool) Event {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream ended before the expected event")
			}
			if matches(ev) {
				return ev
			}
			if ev.Discussion != nil {
				t.Logf("discussion: %+v event=%+v", ev.Discussion, ev.Discussion.Event)
			} else {
				t.Logf("event: %+v", ev)
			}
		case <-timer.C:
			t.Fatal("timed out waiting for discussion event")
		}
	}
}

func TestQuestionConversationClarifiesWhilePermissionWaits(t *testing.T) {
	agent, dir := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("original", "write", `{"path":"original.txt","content":"original"}`), nil
		},
		func(_ context.Context, m []ai.Message) (*ai.Response, error) {
			all := ""
			for _, msg := range m {
				all += messageContentText(msg)
			}
			if !strings.Contains(all, "original.txt") || !strings.Contains(all, "why this file?") {
				t.Errorf("clarification lost context: %s", all)
			}
			return toolResponse("clarifying", "bash", `{"command":"cat facts.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The facts explain the file."), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Original finished."), nil
		},
	}}, func(c *Config) { c.AskConsent = true; c.Interactive = true; c.ApprovalPolicy = promptAll() })
	if err := os.WriteFile(filepath.Join(dir, "facts.txt"), []byte("facts"), 0600); err != nil {
		t.Fatal(err)
	}
	watch, stop := agent.WatchQuestions()
	defer stop()
	main := mustSubmit(t, agent, "write original.txt")
	original := discussionWait(t, watch, func(e Event) bool { return e.Kind == EventQuestion }).Question
	if err := agent.ResolveQuestion(Answer{Kind: original.Kind, ID: original.ID, Clarify: true, AskedBack: []Exchange{{Asked: "why this file?"}}}); err != nil {
		t.Fatal(err)
	}
	nested := discussionWait(t, watch, func(e Event) bool { return e.Kind == EventQuestion && e.Question.ClarificationDepth > 0 }).Question
	if _, err := os.Stat(filepath.Join(dir, "original.txt")); !os.IsNotExist(err) {
		t.Fatal("clarification ran the original action")
	}
	if len(agent.OpenQuestions()) != 2 {
		t.Fatalf("questions: %+v", agent.OpenQuestions())
	}
	if err := agent.ResolveQuestion(Answer{Kind: original.Kind, ID: original.ID, Key: "1"}); err == nil {
		t.Fatal("original answer bypassed the prerequisite")
	}
	if err := agent.ResolveQuestion(Answer{Kind: nested.Kind, ID: nested.ID, Ref: nested.Ref, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	discussionWait(t, watch, func(e Event) bool {
		return e.Discussion != nil && e.Discussion.Event != nil && e.Discussion.Event.Kind == EventTurnDone
	})
	if err := agent.ResolveQuestion(Answer{Kind: original.Kind, ID: original.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	discussionWait(t, main, func(e Event) bool { return e.Kind == EventTurnDone })
	if _, err := os.Stat(filepath.Join(dir, "original.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestQuestionConversationOtherWithdrawsWithoutExecuting(t *testing.T) {
	agent, dir := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("old", "write", `{"path":"old.txt","content":"old"}`), nil
		},
		func(_ context.Context, m []ai.Message) (*ai.Response, error) {
			if !strings.Contains(messageContentText(m[len(m)-1]), "explain instead") {
				t.Error("updated request never reached model")
			}
			return textResponse("Here is the explanation."), nil
		},
	}}, func(c *Config) { c.AskConsent = true; c.Interactive = true; c.ApprovalPolicy = promptAll() })
	watch, stop := agent.WatchQuestions()
	defer stop()
	mustSubmit(t, agent, "write old.txt")
	q := discussionWait(t, watch, func(e Event) bool { return e.Kind == EventQuestion }).Question
	stream, err := agent.ReplaceQuestion(context.Background(), Answer{Kind: q.Kind, ID: q.ID, Change: "explain instead"})
	if err != nil {
		t.Fatal(err)
	}
	discussionWait(t, stream, func(e Event) bool { return e.Kind == EventTurnDone })
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("replacement approved old tool")
	}
	if len(agent.OpenQuestions()) != 0 {
		t.Fatalf("old question remains: %+v", agent.OpenQuestions())
	}
}

func TestQuestionConversationOriginalCanBeAnsweredDuringClarification(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	defer close(finish)
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("original", "write", `{"path":"yes.txt","content":"yes"}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(started)
			select {
			case <-finish:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return textResponse("Explanation"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Original complete"), nil
		},
	}}, func(c *Config) { c.AskConsent = true; c.Interactive = true; c.ApprovalPolicy = promptAll() })
	watch, stop := agent.WatchQuestions()
	defer stop()
	main := mustSubmit(t, agent, "write yes.txt")
	q := discussionWait(t, watch, func(e Event) bool { return e.Kind == EventQuestion }).Question
	if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Clarify: true, AskedBack: []Exchange{{Asked: "explain why"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("clarification did not start")
	}
	if err := agent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	discussionWait(t, main, func(e Event) bool { return e.Kind == EventTurnDone })
	// Approval settings and session grants stay shared while the child runs.
	agent.mu.Lock()
	child := agent.discussions["clarify-1"].child
	agent.mu.Unlock()
	agent.rememberConsent("bash", true)
	if allowed, known := child.rememberedConsent("bash"); !known || !allowed {
		t.Fatal("clarification lost session approvals")
	}
	child.rememberConsent("write", true)
	if allowed, known := agent.rememberedConsent("write"); !known || !allowed {
		t.Fatal("clarification approval did not apply to the session")
	}
}
