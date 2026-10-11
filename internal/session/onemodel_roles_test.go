package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// pinnedRoleCalls records the request model and returns real accounting so the
// regression proves the ledger as well as the role resolver.
type pinnedRoleCalls struct {
	models []string
}

func (c *pinnedRoleCalls) CompleteWithMessages(_ context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	c.models = append(c.models, request.Model)
	return billedCall(request.Model, 10, 2, 0.001), nil
}

func TestOneModelPinsEveryAuxiliaryRoleAndItsUsageLedger(t *testing.T) {
	const pinned = "deepseek/deepseek-v4.1-flash"
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	client := &pinnedRoleCalls{}
	agent, _ := newTestAgent(t, client, func(cfg *Config) {
		cfg.Model = pinned
		cfg.OneModel = true
		cfg.usageLedger = ledger
		// Every inherited pin and tier disagrees with the measured model.
		cfg.RolesSource = func(string) (string, bool) { return "inherited/other-model", true }
	})
	var count int
	for _, role := range roles.Vocabulary() {
		switch role {
		case roles.RoleVision, roles.RoleImageGen, roles.RoleSpeech, roles.RoleVideo:
			continue
		}
		response, model, err := agent.callRole(context.Background(), role, "", nil)
		if err != nil || model != pinned {
			t.Fatalf("role %s = %q, %v; want %q", role, model, err, pinned)
		}
		agent.addAuxiliaryUsageAs(response, model, 1, string(role))
		count++
	}
	FlushUsage()
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(lines) != count || len(client.models) != count {
		t.Fatalf("ledger=%d requests=%d want=%d: %v", len(lines), len(client.models), count, err)
	}
	for i, line := range lines {
		if line.Model != pinned || client.models[i] != pinned {
			t.Errorf("role %s recorded %q and requested %q; want %q", line.Role, line.Model, client.models[i], pinned)
		}
	}
}
