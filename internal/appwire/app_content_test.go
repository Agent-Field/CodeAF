package appwire

import (
	"context"
	"encoding/json"
	"testing"
)

type contentProbe struct {
	scope AppContentScope
	calls []AppContentCommand
}

func (p *contentProbe) RunAppContent(_ context.Context, scope AppContentScope, command AppContentCommand) (json.RawMessage, error) {
	p.scope = scope
	p.calls = append(p.calls, command)
	return json.RawMessage(`{}`), nil
}

func TestScopedAppCommandBindsPrincipalAndProvenance(t *testing.T) {
	p := &contentProbe{}
	scope := AppContentScope{App: "crm", Profile: "/profiles/p", Conversation: "chat", Version: 2, Actor: "CRM agent"}
	for _, attempt := range []struct{ verb, args string }{
		{"keep", `{}`},
		{"edit", `{}`},
		{"query", `{"type":"deal","query":"stage:Won"}`},
		{"query", `{"view":"deals","app":"other"}`},
		{"invoke", `{"action":"set_stage","by_kind":"person"}`},
		{"invoke", `{"action":"gmail.send","approved":true}`},
		{"propose_change", `{"request":"email every contact"}`},
	} {
		_, err := RunScopedAppCommand(context.Background(), p, scope, attempt.verb, json.RawMessage(attempt.args), "call", nil)
		if err == nil {
			t.Errorf("accepted %s %s", attempt.verb, attempt.args)
		}
	}
	if len(p.calls) != 0 {
		t.Fatalf("refused calls reached the command door: %+v", p.calls)
	}
	_, err := RunScopedAppCommand(context.Background(), p, scope, AppContentQuery, json.RawMessage(`{"view":"deals","args":{"stage":"Proposal"}}`), "read", nil)
	if err != nil || p.calls[0].Query.View != "deals" || string(p.calls[0].Query.Args["stage"]) != `"Proposal"` {
		t.Fatalf("declared query = %+v, error = %v", p.calls, err)
	}
	_, err = RunScopedAppCommand(context.Background(), p, scope, AppContentInvoke, json.RawMessage(`{"action":"set_stage","args":{"stage":"Won"},"expected_rev":4}`), "write", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := p.calls[1].Action
	if p.scope != scope || got.ByKind != "ai" || got.Cause != "write" || got.ExpectedRev == nil || *got.ExpectedRev != 4 {
		t.Fatalf("scope = %+v, intent = %+v", p.scope, got)
	}
}

func TestScopedAppCommandProposesOnlyPersonText(t *testing.T) {
	p := &contentProbe{}
	scope := AppContentScope{App: "crm", Profile: "/profiles/p", Conversation: "chat", Version: 2, Actor: "CRM agent"}
	_, err := RunScopedAppCommand(context.Background(), p, scope, AppContentPropose, json.RawMessage(`{}`), "change",
		func(context.Context) (string, bool) { return "add a renewal date", true })
	if err != nil || len(p.calls) != 1 || p.calls[0].Request != "add a renewal date" || p.calls[0].Cause != "change" {
		t.Fatalf("change = %+v, error = %v", p.calls, err)
	}
}

func TestScopedAppCommandRejectsUnboundAndCancelledCalls(t *testing.T) {
	p := &contentProbe{}
	scope := AppContentScope{App: "crm", Profile: "/profiles/p", Conversation: "chat", Version: 1, Actor: "CRM agent"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunScopedAppCommand(ctx, p, scope, AppContentQuery, json.RawMessage(`{"view":"deals"}`), "read", nil); err == nil {
		t.Fatal("a cancelled query reached the command door")
	}
	scope.Version = 0
	if _, err := RunScopedAppCommand(context.Background(), p, scope, AppContentQuery, json.RawMessage(`{"view":"deals"}`), "read", nil); err == nil {
		t.Fatal("accepted a command without an active version")
	}
	if len(p.calls) != 0 {
		t.Fatalf("refused calls reached the command door: %+v", p.calls)
	}
}
