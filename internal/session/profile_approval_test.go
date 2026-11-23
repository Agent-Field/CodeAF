package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
)

func TestUnreadableProfileApprovalRequiresPersonEveryTime(t *testing.T) {
	const file = "/profile/config.json"
	policy := &approval.Policy{ProfileReadError: "open " + file + ": permission denied"}
	agent, runs := consentAgent(t, &scriptedCompleter{steps: toolCallTurn("touch", "touch")}, policy, true)
	agent.rememberConsent("touch", true)
	events := drainAnswering(t, mustSubmit(t, agent, "touch twice"), func(request Event) {
		if !strings.Contains(request.Rule, file) {
			t.Errorf("prompt omits failed file: %s", request.Rule)
		}
		agent.ResolveConsentRemember(request.ID, true, ConsentToolSession)
	})
	if got := countKind(events, EventConsentRequest); got != 2 {
		t.Fatalf("asked %d times; want 2", got)
	}
	if len(runs) != 2 {
		t.Fatalf("ran %d times; want 2 human-approved calls", len(runs))
	}
	for _, tool := range []string{"touch", "read", "bash", "use_service"} {
		agent.rememberConsent(tool, true)
		result, allowed := agent.approve(context.Background(), nil, gateCall(tool, `{"command":"git status"}`))
		if allowed || !result.isError || !strings.Contains(result.text, file) {
			t.Errorf("%s without person: allowed=%v result=%+v", tool, allowed, result)
		}
	}
}

func TestUnreadableProfileApprovalUserBashRefusesWithoutPerson(t *testing.T) {
	const file = "/profile/config.json"
	agent := gateAgent(t, &approval.Policy{ProfileReadError: "open " + file + ": permission denied"})
	for _, args := range []string{`{"command":"git status"}`, `{"command":"rm -rf /"}`} {
		result, allowed := agent.approveUserBash(context.Background(), nil, gateCall("bash", args))
		if allowed || !result.isError || !strings.Contains(result.text, file) {
			t.Errorf("user bash %s: allowed=%v result=%+v; want refusal naming file", args, allowed, result)
		}
	}
}
