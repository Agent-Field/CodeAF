// Serve mode exposes this binary as an AgentField node so other reasoners can
// trigger coding tasks. Go-only additive divergence: the TypeScript CLI has no
// serve command.
package codeaf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/agent"
)

// serveMu serializes executions: the pipeline mutates process-global state
// (PLANDB_DB env, the plandb singleton, CODEAF_CP_* env).
var serveMu sync.Mutex

func runServe(ctx context.Context, stdout, stderr io.Writer) error {
	cpURL := strings.TrimSpace(os.Getenv("AGENTFIELD_URL"))
	if cpURL == "" {
		return errors.New("serve requires AGENTFIELD_URL (control plane base URL)")
	}
	nodeID := envDefault("AGENT_NODE_ID", "swe-pro-go")
	listen := envDefault("AGENT_LISTEN_ADDR", ":8801")
	app, err := agent.New(agent.Config{
		NodeID:        nodeID,
		Version:       version,
		AgentFieldURL: cpURL,
		Token:         os.Getenv("AGENTFIELD_TOKEN"),
		InternalToken: strings.TrimSpace(os.Getenv("AGENTFIELD_AUTHORIZATION_INTERNAL_TOKEN")),
		ListenAddress: listen,
		PublicURL:     envDefault("AGENT_PUBLIC_URL", "http://localhost"+listen),
	})
	if err != nil {
		return err
	}

	app.RegisterReasoner("code_task", func(ctx context.Context, input map[string]any) (any, error) {
		return handleCodeExec(ctx, app, nodeID, cpURL, "run", input, stdout, stderr)
	}, agent.WithDescription(
		"Run an autonomous coding task. Input: goal (required), dir (required "+
			"absolute workspace path), high/low/frontier (model pools), hard, "+
			"pr_ready (bools), max_cost, max_hours (numbers), variant, "+
			"entry_agent. Returns {status, reason, cost_usd, project_id, "+
			"root_task_id, run_id, elapsed_ms}; a failed task is reported in "+
			"status, not as an execution error.",
	))
	app.RegisterReasoner("code_resume", func(ctx context.Context, input map[string]any) (any, error) {
		return handleCodeExec(ctx, app, nodeID, cpURL, "resume", input, stdout, stderr)
	}, agent.WithDescription(
		"Resume a previously failed codeaf run. Input: dir (required), goal "+
			"(required after hard kills, otherwise optional), plus the "+
			"code_task tuning fields. Same result shape as code_task.",
	))

	_, _ = fmt.Fprintf(
		stderr, "[codeaf] serve: node %q listening on %s, control plane %s\n",
		nodeID, listen, cpURL,
	)
	return app.Run(ctx)
}

func handleCodeExec(
	ctx context.Context,
	app *agent.Agent,
	nodeID, cpURL, command string,
	input map[string]any,
	stdout, stderr io.Writer,
) (any, error) {
	goal := stringInput(input, "goal")
	dir := stringInput(input, "dir")
	if dir == "" {
		return nil, errors.New("input.dir is required (workspace path)")
	}
	if command == "run" && goal == "" {
		return nil, errors.New("input.goal is required")
	}
	workspace, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(workspace); statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("input.dir %q is not a directory", workspace)
	}

	serveMu.Lock()
	defer serveMu.Unlock()

	execCtx := agent.ExecutionContextFrom(ctx)
	runID := execCtx.RunID
	if runID == "" {
		runID = fmt.Sprintf("codeaf-%d", time.Now().UnixMilli())
	}
	restore := setenvScoped(map[string]string{
		"CODEAF_CP_URL":    cpURL,
		"CODEAF_CP_RUN":    runID,
		"CODEAF_CP_PARENT": execCtx.ExecutionID,
		"CODEAF_CP_NODE":   nodeID,
	})
	defer restore()

	argv := []string{command}
	if goal != "" {
		argv = append(argv, goal)
	}
	argv = append(argv, "--dir", workspace, "--format", "json")
	for flag, key := range map[string]string{
		"--high": "high", "--low": "low", "--frontier": "frontier",
		"--variant": "variant", "--entry-agent": "entry_agent",
	} {
		if value := stringInput(input, key); value != "" {
			argv = append(argv, flag, value)
		}
	}
	if boolInput(input, "hard") {
		argv = append(argv, "--hard")
	}
	if boolInput(input, "pr_ready") {
		argv = append(argv, "--pr-ready")
	}
	for flag, key := range map[string]string{"--max-cost": "max_cost", "--max-hours": "max_hours"} {
		if value, ok := floatInput(input, key); ok {
			argv = append(argv, flag, fmt.Sprint(value))
		}
	}

	app.Note(ctx, fmt.Sprintf("%s started: %s", command, headText(goal, 120)), "codeaf")

	var captured bytes.Buffer
	started := time.Now()
	runErr := runCLI(ctx, argv, nil, io.MultiWriter(&captured, stdout), stderr)
	elapsed := time.Since(started).Milliseconds()

	result := map[string]any{
		"run_id": runID, "dir": workspace, "elapsed_ms": elapsed,
	}
	terminal, supervised := scanServeEvents(captured.Bytes())
	switch {
	case terminal != nil:
		result["status"] = terminal.Status
		if terminal.Message != "" {
			result["reason"] = terminal.Message
		}
		for key, value := range terminal.Data {
			result[key] = value
		}
	case runErr != nil:
		result["status"] = "crashed"
		result["reason"] = runErr.Error()
	default:
		result["status"] = "unknown"
	}
	// The auto-resume supervisor emits its outcome after the parent's fail
	// terminal; a passed supervision means the run recovered.
	if supervised != nil && supervised.Status == "passed" {
		result["status"] = "pass"
		result["auto_resumed"] = true
	}

	status, _ := result["status"].(string)
	note := fmt.Sprintf("%s finished: %s", command, status)
	if cost, ok := floatInput(result, "cost_usd"); ok {
		note += fmt.Sprintf(" ($%.4f)", cost)
	}
	app.Note(ctx, note, "codeaf")

	if terminal == nil && runErr != nil {
		return result, runErr
	}
	return result, nil
}

// scanServeEvents extracts the last terminal and supervisor events from the
// run's captured NDJSON stream.
func scanServeEvents(raw []byte) (terminal *event, supervised *event) {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var value event
		if err := json.Unmarshal(line, &value); err != nil {
			continue
		}
		switch value.Type {
		case "terminal":
			copied := value
			terminal = &copied
		case "supervisor":
			copied := value
			supervised = &copied
		}
	}
	return terminal, supervised
}

// setenvScoped applies env overrides and returns a restore func.
func setenvScoped(values map[string]string) func() {
	previous := map[string]*string{}
	for key, value := range values {
		if existing, ok := os.LookupEnv(key); ok {
			copied := existing
			previous[key] = &copied
		} else {
			previous[key] = nil
		}
		_ = os.Setenv(key, value)
	}
	return func() {
		for key, value := range previous {
			if value == nil {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, *value)
			}
		}
	}
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func stringInput(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func boolInput(input map[string]any, key string) bool {
	switch value := input[key].(type) {
	case bool:
		return value
	case string:
		return value == "true" || value == "1"
	}
	return false
}

func floatInput(input map[string]any, key string) (float64, bool) {
	switch value := input[key].(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(value, "%g", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func headText(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) > max {
		return text[:max] + "…"
	}
	return text
}
