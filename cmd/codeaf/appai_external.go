package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
)

func checkCodexAppAI(*schema.AIStep) error {
	binary, err := exec.LookPath("codex")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "login", "status")
	command.Env = codexAppEnv()
	status, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(status), "Logged in using ChatGPT") {
		return runs.ErrAIUnavailable
	}
	return nil
}

func runCodexAppAI(ctx context.Context, call runs.AgentCall) (runs.Reply, error) {
	binary, err := exec.LookPath("codex")
	if err != nil {
		return runs.Reply{}, err
	}
	dir, err := os.MkdirTemp("", "codeaf-app-codex-")
	if err != nil {
		return runs.Reply{}, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output := filepath.Join(dir, "answer.json")
	cmd := exec.CommandContext(ctx, binary, "exec",
		"--skip-git-repo-check", "--sandbox", "read-only", "--ephemeral",
		"--ignore-user-config", "--ignore-rules",
		"--disable", "shell_tool", "--disable", "unified_exec",
		"--disable", "multi_agent", "--disable", "multi_agent_v2",
		"-c", "web_search=disabled", "-c", "mcp_servers={}",
		"--output-last-message", output, "-")
	cmd.Dir, cmd.Env = dir, codexAppEnv()
	cmd.Stdin = strings.NewReader(appAISystem(call.AI) + "\n\nApp request and record data:\n" + call.Prompt)
	if err := cmd.Run(); err != nil {
		return runs.Reply{}, errors.Join(runs.ErrOutcomeUnknown, errors.New("Codex did not complete the app request"))
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		return runs.Reply{}, errors.Join(runs.ErrOutcomeUnknown, errors.New("Codex did not leave an app response"))
	}
	// The probe only seats the included subscription road.
	return runs.Reply{Output: string(raw)}, nil
}

// Codex app runs use the subscription that the availability probe verified.
func codexAppEnv() []string {
	return append(os.Environ(), "OPENAI_API_KEY=", "OPENAI_BASE_URL=", "CODEX_API_KEY=")
}

const appAgentFieldCapability = "codeaf-app-typed-output-v1"

func (b *appBackend) checkAgentFieldAppAI(*schema.AIStep) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, _, err := b.appAIReasoner(ctx)
	return err
}

func (b *appBackend) appAIReasoner(ctx context.Context) (*agentfield.Client, string, string, error) {
	target := strings.TrimSpace(os.Getenv("CODEAF_APP_AGENTFIELD_REASONER"))
	node, operation, ok := strings.Cut(target, ".")
	if !ok || node == "" || operation == "" || strings.ContainsAny(target, "/ \r\n") {
		return nil, "", "", runs.ErrAIUnavailable
	}
	client, settings, err := b.agentFieldClient(b.localFactoryID())
	if err != nil {
		return nil, "", "", runs.ErrAIUnavailable
	}
	nodes, err := client.Nodes(ctx)
	if err != nil {
		return nil, "", "", runs.ErrAIUnavailable
	}
	for _, candidate := range nodes {
		if candidate.ID != node || candidate.HealthStatus != "active" {
			continue
		}
		for _, reasoner := range candidate.Reasoners {
			if (reasoner.ID == operation || reasoner.ID == target) && slices.Contains(reasoner.Tags, appAgentFieldCapability) {
				return client, target, settings.NodeID, nil
			}
		}
	}
	return nil, "", "", runs.ErrAIUnavailable
}

func (b *appBackend) runAgentFieldAppAI(ctx context.Context, call runs.AgentCall) (runs.Reply, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client, target, owner, err := b.appAIReasoner(ctx)
	if err != nil {
		return runs.Reply{}, err
	}
	started, err := client.Dispatch(ctx, target, map[string]any{
		"request_id": call.Key, "prompt": call.Prompt, "instructions": appAISystem(call.AI),
		"output_schema": call.AI.Output, "remaining_usd": call.Remaining,
	}, owner)
	if err != nil || started.ExecutionID == "" || started.RunID == "" {
		return runs.Reply{}, runs.ErrOutcomeUnknown
	}
	finished := false
	defer func() {
		if !finished {
			stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = client.CancelTree(stop, started.RunID)
		}
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		root, _, err := client.Tree(ctx, started.RunID)
		if err != nil {
			return runs.Reply{}, runs.ErrOutcomeUnknown
		}
		switch root.Status {
		case agentfield.ExecSucceeded:
			finished = true
			raw, err := client.Result(ctx, started.ExecutionID)
			var reply struct {
				Output json.RawMessage `json:"output"`
				Cost   *float64        `json:"cost_usd"`
			}
			if err != nil || json.Unmarshal(raw, &reply) != nil || reply.Cost == nil || len(reply.Output) == 0 {
				return runs.Reply{}, runs.ErrOutcomeUnknown
			}
			return runs.Reply{Output: string(reply.Output), Cost: *reply.Cost}, nil
		case agentfield.ExecFailed, "cancelled", "canceled":
			finished = true
			return runs.Reply{}, runs.ErrOutcomeUnknown
		}
		select {
		case <-ctx.Done():
			return runs.Reply{}, runs.ErrOutcomeUnknown
		case <-tick.C:
		}
	}
}
