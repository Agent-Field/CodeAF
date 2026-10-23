package session

import (
	"encoding/json"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAutoJobPolicyAllowsOnlyRoutineControlsAndKeepsFloors(t *testing.T) {
	base := &approval.Policy{Default: approval.ActionPrompt, Tools: map[string]approval.Action{"factory_run": approval.ActionDeny}, BashPatterns: []approval.Rule{{Match: "rm *", Action: approval.ActionPrompt}}}
	p := automaticJobPolicy(base)
	for _, tool := range []string{"factory_start", "factory_answer", "team_read", "manual"} {
		if d := p.Check(tool, json.RawMessage(`{}`)); d.Action != approval.ActionAllow {
			t.Errorf("%s=%s", tool, d.Action)
		}
	}
	for _, tool := range []string{"bash", "write", "arbitrary_tool", "mail_send"} {
		if d := p.Check(tool, json.RawMessage(`{"command":"touch file"}`)); d.Action != approval.ActionPrompt {
			t.Errorf("unexpected permission widening: %s=%s", tool, d.Action)
		}
	}
	if d := p.Check("factory_run", nil); d.Action != approval.ActionDeny {
		t.Fatal("overrode explicit denial")
	}
	if base.Tools["factory_start"] != "" || base.Default != approval.ActionPrompt || p.BashPatterns[0].Action != approval.ActionPrompt {
		t.Fatal("modified saved policy or shell rules")
	}
	// Even a pre-existing allow posture continues to ask for critical commands.
	allowed := automaticJobPolicy(&approval.Policy{Default: approval.ActionAllow})
	if d := allowed.Check("bash", json.RawMessage(`{"command":"rm -rf /"}`)); d.Action != approval.ActionPrompt {
		t.Fatalf("critical floor=%s", d.Action)
	}
}

type autonomyMeter struct{ on atomic.Bool }

func (*autonomyMeter) Spend(float64)       {}
func (*autonomyMeter) Refusal() string     { return "" }
func (m *autonomyMeter) FactoryAuto() bool { return m.on.Load() }

func TestAutoModeChangesTheNextInternalToolWithoutRestart(t *testing.T) {
	meter := &autonomyMeter{}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
		cfg.JobBudget = meter
	})
	call := gateCall("factory_start", `{}`)
	for _, enabled := range []bool{false, true, false} {
		meter.on.Store(enabled)
		got, _ := agent.decide(call)
		want := approval.ActionPrompt
		if enabled {
			want = approval.ActionAllow
		}
		if got.Action != want {
			t.Fatalf("Auto=%v decision=%s want=%s", enabled, got.Action, want)
		}
	}
	denied := automaticJobPolicy(&approval.Policy{Default: approval.ActionDeny})
	if got := denied.Check("factory_start", nil); got.Action != approval.ActionDeny {
		t.Fatal("Auto overrode blanket explicit deny")
	}
}

func TestFactoryAutoInstructionFollowsLiveModeWithoutEditingHistory(t *testing.T) {
	meter := &autonomyMeter{}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.JobBudget = meter })
	original := []ai.Message{textMessage("system", "base instruction"), textMessage("user", "continue")}
	for _, enabled := range []bool{false, true, false} {
		meter.on.Store(enabled)
		actual := agent.jobAutonomyMessages(original)
		if strings.Contains(messageContentText(actual[0]), jobAutoInstruction) != enabled {
			t.Fatalf("request did not follow Auto=%v", enabled)
		}
		if messageContentText(original[0]) != "base instruction" {
			t.Fatal("mode instructions changed persistent history")
		}
	}
}
