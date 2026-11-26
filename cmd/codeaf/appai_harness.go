package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/claudeagent"
)

func (b *appBackend) appAIHarnesses(files runs.Files) runs.Harnesses {
	return runs.Harnesses{
		runs.HarnessCodeaf:     {Check: b.checkCodeafAI, Run: b.appRunAgent(files)},
		runs.HarnessClaudeCode: {Check: checkClaudeAppAI, Run: runClaudeAppAI},
	}
}

func checkClaudeAppAI(*schema.AIStep) error {
	// Tool-free mode uses API-key authentication. A signed-in interactive
	// CLI without that credential cannot enforce this invocation's scope.
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		return runs.ErrAIUnavailable
	}
	_, err := claudeagent.Binary()
	return err
}

func runClaudeAppAI(ctx context.Context, call runs.AgentCall) (runs.Reply, error) {
	binary, err := claudeagent.Binary()
	if err != nil {
		return runs.Reply{}, err
	}
	dir, err := os.MkdirTemp("", "codeaf-app-claude-")
	if err != nil {
		return runs.Reply{}, err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, binary, "--print", "--bare", "--tools", "",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--setting-sources", "", "--disable-slash-commands", "--no-session-persistence",
		"--output-format", "json", "--max-turns", "1", "--max-budget-usd", strconv.FormatFloat(call.Remaining, 'f', -1, 64),
		"--system-prompt", appAISystem(call.AI))
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(call.Prompt)
	raw, err := cmd.Output()
	var answer struct {
		Cost    float64 `json:"total_cost_usd"`
		Text    string  `json:"result"`
		Subtype string  `json:"subtype"`
	}
	if json.Unmarshal(raw, &answer) != nil {
		return runs.Reply{}, runs.ErrOutcomeUnknown
	}
	reply := runs.Reply{Output: answer.Text, Cost: answer.Cost}
	if answer.Subtype == "error_max_budget_usd" {
		return reply, runs.ErrBudget
	}
	if err != nil || answer.Subtype != "success" {
		return reply, errors.Join(runs.ErrOutcomeUnknown, errors.New("Claude Code did not complete the app request"))
	}
	return reply, nil
}
