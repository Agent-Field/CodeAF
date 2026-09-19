package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestRoleOrganizeGoesThroughCallRoleChecked(t *testing.T) {
	tier, ok := roles.TierOf(roles.RoleOrganize)
	if !ok {
		t.Fatal("RoleOrganize is not registered")
	}
	if tier != roles.TierLow {
		t.Fatalf("RoleOrganize sits on %q, want low", tier)
	}
	if roles.RoleOrganize == roles.RoleAuditor {
		t.Fatal("RoleOrganize must never be RoleAuditor")
	}

	var seen [][]ai.Message
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			seen = append(seen, messages)
			return textResponse(`{"kind":"no-action","chat_id":"c","source_rev":"1","actions":[]}`), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	plan, err := agent.Organize(context.Background(), OrganizeRequest{
		ChatID: "c", SourceRev: "1", Evidence: "hit 12 cites the billing thread",
	})
	if err != nil {
		t.Fatalf("Organize: %v", err)
	}
	if plan.Kind != PlanNoAction {
		t.Fatalf("kind %q, want %q", plan.Kind, PlanNoAction)
	}
	if completer.model(0) != "test/model" {
		t.Fatalf("routine organize rode %q, want the session model", completer.model(0))
	}
	if len(seen) == 0 {
		t.Fatal("Organize never reached the session completer — it must go through callRoleChecked")
	}
	sys := messageText(seen[0][0])
	if !strings.Contains(sys, "cited evidence") {
		t.Fatalf("organize system prompt missing:\n%s", sys)
	}
	if strings.Contains(strings.ToLower(sys), "auditor") {
		t.Fatalf("organize routed as auditor:\n%s", sys)
	}

	high := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"kind":"add","chat_id":"c","source_rev":"2","actions":[]}`), nil
		},
	}}
	raised, _ := newTestAgent(t, high, func(config *Config) {
		config.RolesSource = func(key string) (string, bool) {
			switch key {
			case roles.TierKey(roles.TierLow):
				return "cheap/model", true
			case roles.TierKey(roles.TierHigh):
				return "high/model", true
			}
			return "", false
		}
	})
	plan, err = raised.Organize(context.Background(), OrganizeRequest{
		ChatID: "c", SourceRev: "2", Evidence: "conflict", High: true,
	})
	if err != nil {
		t.Fatalf("high Organize: %v", err)
	}
	if plan.Kind != PlanAdd {
		t.Fatalf("high kind %q, want %q", plan.Kind, PlanAdd)
	}
	if high.model(0) != "high/model" {
		t.Fatalf("restructuring organize rode %q, want the high-tier model", high.model(0))
	}
}
