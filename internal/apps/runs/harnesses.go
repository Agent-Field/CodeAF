package runs

import (
	"context"
	"slices"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const (
	HarnessCodeaf     = "codeaf"
	HarnessClaudeCode = "claude_code"
	HarnessCodex      = "codex"
	HarnessAgentField = "agentfield"
)

// Harness pairs an invocation with the probe for its particular model and
// scope. A missing adapter means the capabilities of that harness cannot be
// enforced in this app run, even if its executable is installed.
type Harness struct {
	Check func(*schema.AIStep) error
	Run   func(context.Context, AgentCall) (Reply, error)
}

type Harnesses map[string]Harness

func harnessID(step *schema.AIStep) string {
	if step.Harness == "" {
		return HarnessCodeaf
	}
	return step.Harness
}

func (h Harnesses) Check(step *schema.AIStep) error {
	harness := h[harnessID(step)]
	if harness.Check == nil || harness.Run == nil || harness.Check(step) != nil {
		return ErrAIUnavailable
	}
	return nil
}

func (h Harnesses) Run(ctx context.Context, call AgentCall) (Reply, error) {
	if err := h.Check(call.AI); err != nil {
		return Reply{}, err
	}
	return h[harnessID(call.AI)].Run(ctx, call)
}

func (h Harnesses) Available() []string {
	available := []string{}
	for _, id := range []string{HarnessCodeaf, HarnessClaudeCode, HarnessCodex, HarnessAgentField} {
		if h.Check(&schema.AIStep{Harness: id}) == nil {
			available = append(available, id)
		}
	}
	slices.Sort(available)
	return available
}
