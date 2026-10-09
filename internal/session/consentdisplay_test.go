package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
)

func TestConsentCommandSeparatesActualCompoundFromPolicyPattern(t *testing.T) {
	a := &Agent{config: Config{Workspace: "/tmp/owned workspace"}}
	command := "rm -rf /tmp/stars && mkdir -p /tmp/stars\nprintf 'whole command'"
	args, _ := json.Marshal(map[string]string{"command": command})
	call := ai.ToolCall{ID: "actual-call", Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)}}
	q := a.consentAsk(1, call, approval.Decision{Rule: `critical command "rm -rf /*"`})
	if len(q.Attach) != 2 || q.Attach[0].Body != command || q.Attach[1].Body != a.config.Workspace {
		t.Fatalf("incomplete command evidence: %+v", q.Attach)
	}
	if q.Subject.CallID != call.ID || !strings.Contains(q.Reason, "pattern") || !strings.Contains(q.Reason, "only to this request") {
		t.Fatalf("misleading request identity: %+v", q)
	}
	if len(q.Scope) != 1 || q.Scope[0] != ScopeOnce {
		t.Fatalf("critical request widened: %+v", q.Scope)
	}
	for _, o := range q.Options {
		if o.Widening {
			t.Fatal("critical command offers standing approval")
		}
	}
}

func TestConsentCommandRedactsSecretsWithoutClippingCommand(t *testing.T) {
	a := &Agent{config: Config{Workspace: "/tmp/owned"}}
	secret := "sk-abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKL"
	command := "TOKEN=" + secret + " printf done && " + strings.Repeat("printf x; ", 1000)
	args, _ := json.Marshal(map[string]string{"command": command})
	blocks := a.consentCommand(ai.ToolCall{Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)}})
	if strings.Contains(blocks[0].Body, secret) || !strings.Contains(blocks[0].Title, "secrets hidden") || !strings.HasSuffix(blocks[0].Body, strings.Repeat("printf x; ", 1000)) {
		t.Fatal("command leaks secrets or was clipped")
	}
}

func TestConsentOnceAnswersOnlyTheExactPendingCall(t *testing.T) {
	a, _ := questionSession(t, "consentonce1111", nil)
	hub := newEventHub()
	events := hub.subscribe()
	type result struct {
		call   string
		answer consentAnswer
	}
	done := make(chan result, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := func(id, command string) {
		go func() {
			args, _ := json.Marshal(map[string]string{"command": command})
			answer, _ := a.askAnswer(ctx, hub, ai.ToolCall{ID: id, Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)}}, approval.Decision{Rule: `critical command "rm -rf /*"`})
			done <- result{id, answer}
		}()
	}
	start("call-A", "rm -rf /tmp/owned-A")
	first := <-events
	start("call-B", "rm -rf /tmp/owned-B")
	second := <-events
	if err := a.ResolveQuestion(Answer{Kind: QuestionConsent, ID: first.ID, Key: "2"}); err == nil {
		t.Fatal("forged hidden Always answer was accepted")
	}
	if err := a.ResolveQuestion(Answer{Kind: QuestionConsent, ID: first.ID, Key: "1"}); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.call != "call-A" || !got.answer.allow || got.answer.scope != ConsentOnce {
		t.Fatalf("wrong call approved: %+v", got)
	}
	select {
	case got := <-done:
		t.Fatalf("once released another call: %+v", got)
	case <-time.After(30 * time.Millisecond):
	}
	a.ResolveConsent(first.ID, true)
	select {
	case got := <-done:
		t.Fatalf("replay released another call: %+v", got)
	case <-time.After(30 * time.Millisecond):
	}
	a.ResolveConsent(second.ID, false)
	if got := <-done; got.call != "call-B" || got.answer.allow {
		t.Fatalf("wrong second resolution: %+v", got)
	}
	if _, known := a.rememberedConsent("bash"); known {
		t.Fatal("once created a broad bash grant")
	}
}
