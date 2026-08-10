// This file exercises the assembly contract for swe-pro/src/cli/cmd/run.ts:495-2913.
package codeaf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
)

type offlineBackend struct {
	t *testing.T

	mu         sync.Mutex
	leafStarts int
	leafActive int
	maxActive  int
	toolCalls  int
	auditCalls int
	auditFails int
	bothLeaves chan struct{}
}

func (backend *offlineBackend) Run(ctx context.Context, request turn) (turnResult, error) {
	switch request.Agent {
	case "input-classifier":
		return turnResult{Text: "classified"}, backend.scriptedJSON(ctx, request,
			filepath.Join(request.Workspace, ".codeaf", "plan", "classification.json"),
			map[string]any{"class": "focused", "reason": "bounded implementation"},
		)
	case "architect":
		body := strings.Join([]string{
			"# Architecture",
			"",
			"## Components",
			"- alpha: writes alpha.txt",
			"- beta: writes beta.txt",
			"- gamma: integrates alpha and beta into gamma.txt",
			"",
			"## File layout",
			"- alpha.txt",
			"- beta.txt",
			"- gamma.txt",
			"",
			"## Module dependency graph",
			"alpha and beta are independent; gamma depends on both.",
		}, "\n")
		return turnResult{Text: "architecture written"}, backend.scriptedWrite(ctx, request,
			filepath.Join(request.Workspace, ".codeaf", "plan", "architecture.md"),
			body,
		)
	case "planner-translate":
		dag := map[string]any{
			"summary": "two independent leaves",
			"tasks": []map[string]any{
				{
					"taskKey": "alpha", "title": "Implement alpha",
					"kind": "code", "description": "file_scope: alpha.txt\nWrite alpha.",
					"tags": []string{"agent:fixer", "scope:small"}, "deps": []any{},
				},
				{
					"taskKey": "beta", "title": "Implement beta",
					"kind": "code", "description": "file_scope: beta.txt\nWrite beta.",
					"tags": []string{"agent:fixer", "scope:small"}, "deps": []any{},
				},
				{
					"taskKey": "gamma", "title": "Implement gamma",
					"kind": "code", "description": "file_scope: gamma.txt\nIntegrate alpha and beta.",
					"tags": []string{"agent:fixer", "scope:small"},
					"deps": []map[string]any{
						{"from_task": "alpha", "kind": "feeds_into"},
						{"from_task": "beta", "kind": "feeds_into"},
					},
				},
			},
		}
		return turnResult{Text: "DAG written"}, backend.scriptedJSON(ctx, request,
			filepath.Join(request.Workspace, ".codeaf", "plan", "dag.json"), dag,
		)
	case "issue-writer":
		output := pathAfter(request.Prompt, "Output path: ")
		if output == "" {
			return turnResult{}, fmt.Errorf("issue writer prompt has no output path")
		}
		return turnResult{Text: "issue written"}, backend.scriptedWrite(ctx, request,
			output,
			"# Issue\n\n## Acceptance criteria\n- [ ] expected file exists\n",
		)
	case "root-orchestrator":
		return backend.runRootOrchestrator(ctx, request)
	case "fix-generator":
		output := betweenLast(
			request.Prompt,
			"Write a single JSON FixGeneratorDecision object to ",
			" per the contract in your role.",
		)
		if output == "" {
			return turnResult{}, fmt.Errorf("fix-generator prompt has no output path")
		}
		decision := map[string]any{
			"action": "dispatch_fixes", "reason": "repair the audit blocker",
			"summary": "add one focused fix task",
			"fixes": []map[string]any{{
				"title": "Implement delta", "kind": "code", "deps": []any{},
				"description": "file_scope: delta.txt\nWrite the missing delta fix.",
			}},
		}
		return turnResult{Text: "fix plan written"}, backend.scriptedJSON(
			ctx, request, output, decision,
		)
	case "fixer":
		return backend.runLeaf(ctx, request)
	case "superpowers-code-reviewer":
		return turnResult{Text: "review passed"}, backend.scriptedJSON(ctx, request,
			filepath.Join(request.Workspace, ".codeaf", "review-verdict.json"),
			map[string]any{
				"verdict": "pass", "confidence": "high", "done": true,
				"spec_coverage": "the scoped file is present",
				"bugs":          []any{}, "repair_hints": []any{},
				"evidence": "inspected the committed diff",
			},
		)
	case "auditor", "auditor-light":
		backend.mu.Lock()
		backend.auditCalls++
		failing := backend.auditCalls <= backend.auditFails
		backend.mu.Unlock()
		reproduced := true
		notes := "both merged files verified"
		commands := []any{
			map[string]any{"cmd": "go test ./...", "exit": 0},
		}
		verdict := "pass"
		blockers := []any{}
		repairHints := []any{}
		if failing {
			verdict = "fail"
			reproduced = false
			notes = "delta.txt is missing"
			commands = []any{map[string]any{"cmd": "test -f delta.txt", "exit": 1}}
			blockers = []any{map[string]any{
				"step": 2, "detail": "delta.txt is missing", "severity": "correctness",
			}}
			repairHints = []any{"create and verify delta.txt"}
		}
		return turnResult{Text: "audit passed"}, backend.scriptedJSON(ctx, request,
			filepath.Join(request.Workspace, ".codeaf", "auditor-verdict.json"),
			map[string]any{
				"verdict": verdict, "commands": commands,
				"notes": notes,
				"step2_signal": map[string]any{
					"reproduced": reproduced, "commands": commands,
					"spec_examples_matched": true, "notes": notes,
				},
				"blockers": blockers, "repair_hints": repairHints,
			},
		)
	default:
		return turnResult{}, fmt.Errorf("offline backend has no script for agent %q", request.Agent)
	}
}

func (backend *offlineBackend) runRootOrchestrator(
	ctx context.Context, request turn,
) (turnResult, error) {
	if request.Store == nil || request.Scheduler == nil || request.PlanDB == nil {
		return turnResult{}, fmt.Errorf("root turn missing loop seams")
	}
	turns := 0
	for {
		turns++
		finish := "stop"
		assistant := msgmodel.Assistant{
			MessageBase: msgmodel.MessageBase{
				ID: steploop.NewAscendingID("msg"), SessionID: request.SessionID,
			},
			ParentID: request.PromptMessageID, Agent: request.Agent,
			ModelID: request.ModelID, ProviderID: request.ProviderID,
			Finish: &finish,
		}
		if err := request.Store.UpdateMessage(ctx, assistant); err != nil {
			return turnResult{}, err
		}
		summary, err := request.Scheduler.Pump(ctx, steploop.SchedulerInput{
			SessionID: request.SessionID,
			Plan: steploop.PlanDBInfo{
				ProjectID: request.PlanDB.ProjectID, RootTaskID: request.PlanDB.RootTaskID,
				DBPath: request.PlanDB.DBPath,
			},
		})
		if err != nil {
			return turnResult{}, err
		}
		if summary == "" {
			break
		}
		synthetic := true
		user := msgmodel.User{
			MessageBase: msgmodel.MessageBase{
				ID: steploop.NewAscendingID("msg"), SessionID: request.SessionID,
			},
			Agent: request.Agent,
			Model: msgmodel.UserModel{
				ProviderID: request.ProviderID, ModelID: request.ModelID,
			},
		}
		part := msgmodel.TextPart{
			PartBase: msgmodel.PartBase{
				ID: steploop.NewAscendingID("prt"), SessionID: request.SessionID,
				MessageID: user.ID,
			},
			Text: summary, Synthetic: &synthetic,
		}
		if err := request.Store.UpdateMessage(ctx, user); err != nil {
			return turnResult{}, err
		}
		if err := request.Store.UpdatePart(ctx, part); err != nil {
			return turnResult{}, err
		}
		if turns == 32 {
			return turnResult{}, fmt.Errorf("offline root loop did not drain")
		}
	}
	return turnResult{
		SessionID: request.SessionID, Text: fmt.Sprintf("orchestrated %d turn(s)", turns),
	}, nil
}

func (backend *offlineBackend) runLeaf(ctx context.Context, request turn) (turnResult, error) {
	instance, ok := project.FromContext(ctx)
	if !ok || filepath.Clean(instance.Directory) != filepath.Clean(request.Workspace) {
		return turnResult{}, fmt.Errorf(
			"leaf instance cwd = %q, request workspace = %q",
			instance.Directory, request.Workspace,
		)
	}
	name := "alpha"
	switch {
	case strings.Contains(strings.ToLower(request.SessionTitle), "delta"):
		name = "delta"
	case strings.Contains(strings.ToLower(request.SessionTitle), "gamma"):
		name = "gamma"
	case strings.Contains(strings.ToLower(request.SessionTitle), "beta"):
		name = "beta"
	}
	backend.mu.Lock()
	backend.leafStarts++
	backend.leafActive++
	if backend.leafActive > backend.maxActive {
		backend.maxActive = backend.leafActive
	}
	if backend.leafStarts == 2 {
		close(backend.bothLeaves)
	}
	backend.mu.Unlock()
	select {
	case <-backend.bothLeaves:
	case <-ctx.Done():
		return turnResult{}, ctx.Err()
	case <-time.After(10 * time.Second):
		return turnResult{}, fmt.Errorf("parallel leaf %s timed out waiting for sibling", name)
	}
	if err := backend.scriptedWrite(
		ctx, request, filepath.Join(request.Workspace, name+".txt"), name+"\n",
	); err != nil {
		return turnResult{}, err
	}
	if err := backend.scriptedBash(
		ctx, request,
		"git add -- "+name+".txt && git commit -m 'implement "+name+"'",
	); err != nil {
		return turnResult{}, err
	}
	backend.mu.Lock()
	backend.leafActive--
	backend.mu.Unlock()
	passed := true
	return turnResult{
		Text: "implemented " + name, TestPassed: &passed,
	}, nil
}

func (backend *offlineBackend) scriptedWrite(
	ctx context.Context, request turn, path, content string,
) error {
	input, _ := json.Marshal(map[string]any{"filePath": path, "content": content})
	return backend.scriptedCall(ctx, request, "write", input)
}

func (backend *offlineBackend) scriptedJSON(
	ctx context.Context, request turn, path string, value any,
) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return backend.scriptedWrite(ctx, request, path, string(raw))
}

func (backend *offlineBackend) scriptedBash(
	ctx context.Context, request turn, command string,
) error {
	input, _ := json.Marshal(map[string]any{"command": command})
	return backend.scriptedCall(ctx, request, "bash", input)
}

func (backend *offlineBackend) scriptedCall(
	ctx context.Context, request turn, name string, input json.RawMessage,
) error {
	if request.Execute == nil {
		return fmt.Errorf("agent %s has no tool executor", request.Agent)
	}
	backend.mu.Lock()
	backend.toolCalls++
	callID := fmt.Sprintf("offline-%d", backend.toolCalls)
	backend.mu.Unlock()
	result, err := request.Execute(ctx, steploop.ToolCall{
		ID: callID, Name: name, Input: input,
	})
	if err != nil {
		return err
	}
	if strings.Contains(result.Output, "exit status ") {
		return fmt.Errorf("%s failed: %s", name, result.Output)
	}
	return nil
}

func TestOfflineFullPipelineFansOutMergesAndAudits(t *testing.T) {
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_AUTORESUME", "0")
	t.Setenv("CODEAF_FRONTIER", "0")
	t.Setenv("CODEAF_LARGE_BAND", "0")
	t.Setenv("CODEAF_OUTCOME_CACHE", "0")
	t.Setenv("CODEAF_PLANDB_PERSIST", "0")
	t.Setenv("CODEAF_REVIEW_REPAIR_CAP", "0")
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)

	workspace := t.TempDir()
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.name", "codeaf-smoke"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.email", "codeaf@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "README.md"), "base\n"); err != nil {
		t.Fatal(err)
	}
	// Validation contracts 1 and 2: the first full test entrypoint is red,
	// routes through the normal audit-fix loop, and becomes green after delta.
	packageJSON := `{"scripts":{"build":"test -f alpha.txt && test -f beta.txt && test -f gamma.txt","test":"test -f delta.txt"}}`
	if err := writeFile(filepath.Join(workspace, "package.json"), packageJSON); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "README.md", "package.json"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	backend := &offlineBackend{t: t, bothLeaves: make(chan struct{})}
	args, err := parseArgs([]string{
		"run", "--dir", workspace,
		"Implement two independent components with separate files and verify the integrated result." +
			strings.Repeat(" context", 160) + strings.Repeat("\n# Context", 16),
	})
	if err != nil {
		t.Fatal(err)
	}
	events := newEventWriter(&output)
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: backend, Events: events,
	})
	result, err := runner.run(context.Background(), args.Message, pipelineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pass" {
		t.Fatalf("status = %s (%s), events:\n%s", result.Status, result.Reason, output.String())
	}
	if backend.auditCalls != 1 {
		t.Fatalf("auditor calls = %d, want one after the machine-red cycle", backend.auditCalls)
	}
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		raw, readErr := os.ReadFile(filepath.Join(workspace, name+".txt"))
		if readErr != nil || string(raw) != name+"\n" {
			t.Fatalf("%s merge = %q, %v", name, raw, readErr)
		}
	}
	verdict, readErr := auditorgate.ReadVerdictFile(workspace)
	if readErr != nil || verdict == nil {
		t.Fatalf("recorded verdict = %#v, %v", verdict, readErr)
	}
	foundMachineTest := false
	if verdict.Step2Signal != nil {
		for _, command := range verdict.Step2Signal.Commands {
			if auditorgate.CommandName(command) == "npm test" && auditorgate.CommandExit(command) == "0" {
				foundMachineTest = true
			}
		}
	}
	if !foundMachineTest {
		t.Fatalf("final verdict omitted harness npm test evidence: %#v", verdict.Step2Signal)
	}
	if backend.maxActive < 2 {
		t.Fatalf("max concurrent leaf calls = %d, want at least 2", backend.maxActive)
	}
	if backend.toolCalls < 9 {
		t.Fatalf("scripted tool calls = %d, want phase and leaf writes", backend.toolCalls)
	}
	messages, err := runner.runtime.durable.Messages(context.Background(), runner.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 10 {
		t.Fatalf("root transcript messages = %d, want 10", len(messages))
	}
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		switch message.Info.(type) {
		case msgmodel.User:
			roles = append(roles, "user")
		case msgmodel.Assistant:
			roles = append(roles, "assistant")
		}
	}
	if strings.Join(roles, ",") != "user,assistant,user,assistant,user,assistant,user,assistant,user,assistant" {
		t.Fatalf("root transcript roles = %v", roles)
	}
	initialText := messages[0].Parts[0].(msgmodel.TextPart).Text
	if !strings.Contains(initialText, "Task graph pre-populated (DO NOT REPLAN)") {
		t.Fatalf("root initial prompt omitted pre-gate handoff: %s", initialText)
	}
	summary := messages[2].Parts[0].(msgmodel.TextPart)
	if summary.Synthetic == nil || !*summary.Synthetic ||
		!strings.Contains(summary.Text, "PlanDB scheduler completed dispatches") {
		t.Fatalf("root scheduler summary = %#v", summary)
	}
	secondSummary := messages[4].Parts[0].(msgmodel.TextPart)
	if secondSummary.Synthetic == nil || !*secondSummary.Synthetic ||
		!strings.Contains(secondSummary.Text, "PlanDB scheduler completed dispatches") {
		t.Fatalf("second-wave scheduler summary = %#v", secondSummary)
	}
	auditFixPrompt := messages[6].Parts[0].(msgmodel.TextPart)
	auditFixSummary := messages[8].Parts[0].(msgmodel.TextPart)
	if !strings.Contains(auditFixPrompt.Text, "# Audit fix cycle 1") ||
		auditFixSummary.Synthetic == nil || !*auditFixSummary.Synthetic ||
		!strings.Contains(auditFixSummary.Text, "PlanDB scheduler completed dispatches") {
		t.Fatalf("audit-fix root loop prompt=%#v summary=%#v", auditFixPrompt, auditFixSummary)
	}
	root := plandb.GetPlanDB().GetTask(result.RootID)
	if root == nil || root.Status != plandb.StatusDone {
		t.Fatalf("root terminal state = %#v, want done", root)
	}
	stages := eventStages(t, output.Bytes())
	assertOrderedStages(t, stages, []string{
		"root-cut", "classifier", "architecture", "planner",
		"issue-writer", "plan-apply", "root-orchestrator", "scheduler", "audit",
		"fix-generator", "scheduler", "audit",
	})
}

func TestOfflineResumeReentersRootOrchestratorLoop(t *testing.T) {
	// Validation contract: resume skips planning, starts a fresh durable root
	// transcript over the persisted DAG, and receives scheduler summaries.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	t.Setenv("CODEAF_AUTORESUME", "0")
	t.Setenv("CODEAF_FRONTIER", "0")
	t.Setenv("CODEAF_OUTCOME_CACHE", "0")
	t.Setenv("CODEAF_PLANDB_PERSIST", "0")
	t.Setenv("CODEAF_REVIEW_REPAIR_CAP", "0")
	plandb.ResetPlanDBForTesting()
	t.Cleanup(plandb.ResetPlanDBForTesting)

	workspace := t.TempDir()
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.name", "codeaf-smoke"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "config", "user.email", "codeaf@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "README.md"), "base\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "Makefile"), "build:\n\t@true\n\ntest:\n\t@true\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "README.md", "Makefile"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}

	db := plandb.GetPlanDB()
	projectRow := db.Init("codeaf-prior-session", "resume goal")
	rootDescription := "persisted root"
	root, err := db.AddTask(plandb.AddTaskInput{
		Title: "Persisted root", Description: &rootDescription,
		Project: projectRow.ID, Tags: []string{"codeaf:root", "session:prior"},
	})
	if err != nil {
		t.Fatal(err)
	}
	db.ClaimTask(plandb.TaskID(root.ID), "orchestrator")
	db.StartTask(plandb.TaskID(root.ID))
	for _, name := range []string{"alpha", "beta"} {
		description := "file_scope: " + name + ".txt\nWrite " + name + "."
		if _, err := db.AddTask(plandb.AddTaskInput{
			Title: "Implement " + name, Description: &description,
			Project: projectRow.ID, Parent: root.ID,
			Tags: []string{"agent:fixer", "scope:small"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	var output bytes.Buffer
	backend := &offlineBackend{t: t, bothLeaves: make(chan struct{})}
	args, err := parseArgs([]string{
		"resume", "--dir", workspace, "Finish the persisted work",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&output),
	})
	t.Cleanup(runner.runtime.Close)
	seed := "# Resumed run (fresh context)\n\nTwo persisted fix tasks remain."
	result, err := runner.run(context.Background(), args.Message, pipelineOptions{
		Resume: true, ResumeSeed: seed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "pass" || result.ProjectID != projectRow.ID || result.RootID != root.ID {
		t.Fatalf("resume result = %#v", result)
	}
	if backend.maxActive < 2 {
		t.Fatalf("resume max concurrent leaves = %d, want at least 2", backend.maxActive)
	}
	messages, err := runner.runtime.durable.Messages(context.Background(), runner.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("resume root transcript messages = %d, want 4", len(messages))
	}
	initial := messages[0].Parts[0].(msgmodel.TextPart)
	summary := messages[2].Parts[0].(msgmodel.TextPart)
	if initial.Text != seed || summary.Synthetic == nil || !*summary.Synthetic ||
		!strings.Contains(summary.Text, "PlanDB scheduler completed dispatches") {
		t.Fatalf("resume transcript initial=%#v summary=%#v", initial, summary)
	}
	assertOrderedStages(t, eventStages(t, output.Bytes()), []string{
		"resume", "root-orchestrator", "scheduler", "audit",
	})
}

func eventStages(t *testing.T, raw []byte) []string {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	out := []string{}
	for _, line := range lines {
		var value event
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatalf("invalid NDJSON event %q: %v", line, err)
		}
		if value.Stage != "" {
			out = append(out, value.Stage)
		}
	}
	return out
}

func assertOrderedStages(t *testing.T, got, want []string) {
	t.Helper()
	at := 0
	for _, stage := range got {
		if at < len(want) && stage == want[at] {
			at++
		}
	}
	if at != len(want) {
		t.Fatalf("stage order = %v, missing ordered suffix %v", got, want[at:])
	}
}

func pathAfter(prompt, marker string) string {
	index := strings.LastIndex(prompt, marker)
	if index < 0 {
		return ""
	}
	value := prompt[index+len(marker):]
	if newline := strings.IndexByte(value, '\n'); newline >= 0 {
		value = value[:newline]
	}
	return strings.TrimSpace(value)
}

func betweenLast(value, before, after string) string {
	index := strings.LastIndex(value, before)
	if index < 0 {
		return ""
	}
	value = value[index+len(before):]
	end := strings.Index(value, after)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(value[:end])
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func gitRun(directory string, args ...string) error {
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=codeaf-smoke",
		"GIT_AUTHOR_EMAIL=codeaf@example.test",
		"GIT_COMMITTER_NAME=codeaf-smoke",
		"GIT_COMMITTER_EMAIL=codeaf@example.test",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return nil
}
