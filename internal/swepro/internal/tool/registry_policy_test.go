package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/permission"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
)

func registryTestJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type permissionEvaluatorFunc func(permission.AskInput) error

func (fn permissionEvaluatorFunc) Evaluate(input permission.AskInput) error { return fn(input) }

func TestRegistryPermissionDenyBlocksAndAskAutoApproves(t *testing.T) {
	workspace := t.TempDir()
	readTarget := filepath.Join(workspace, "secret.txt")
	if err := os.WriteFile(readTarget, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	rules := permission.Ruleset{
		{Permission: "read", Pattern: readTarget, Action: permission.ActionDeny},
		{Permission: "bash", Pattern: "git status", Action: permission.ActionDeny},
		{Permission: "edit", Pattern: "blocked.txt", Action: permission.ActionDeny},
		{Permission: "edit", Pattern: "asked.txt", Action: permission.ActionAsk},
	}
	registry := NewWithOptions(workspace, RegistryOptions{
		PermissionRules: func(context.Context, steploop.ToolCall) permission.Ruleset {
			return rules
		},
	})

	// Validation contract A: deny returns the TS-shaped rejection before mutation.
	_, err := execute(t, registry, "write", map[string]any{
		"filePath": filepath.Join(workspace, "blocked.txt"), "content": "blocked",
	})
	var denied permission.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("write error = %T %v, want permission.DeniedError", err, err)
	}
	if !strings.HasPrefix(err.Error(), "The user has specified a rule which prevents you from using this specific tool call.") {
		t.Fatalf("denial text = %q", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "blocked.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("denied write changed filesystem: %v", statErr)
	}
	if _, err := execute(t, registry, "read", map[string]any{"filePath": readTarget}); !errors.As(err, &denied) {
		t.Fatalf("configured read error = %T %v, want permission.DeniedError", err, err)
	}
	if _, err := execute(t, registry, "bash", map[string]any{
		"command": "git status && echo should-not-run",
	}); !errors.As(err, &denied) {
		t.Fatalf("configured parsed bash error = %T %v, want permission.DeniedError", err, err)
	}

	// Validation contract A: unattended literal ask is auto-approved like TS.
	if _, err := execute(t, registry, "write", map[string]any{
		"filePath": filepath.Join(workspace, "asked.txt"), "content": "approved",
	}); err != nil {
		t.Fatalf("ask policy blocked unattended write: %v", err)
	}
}

func TestRegistryMutationPermissionReceivesProposedDiffBeforeWrite(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "proposed.txt")
	var request permission.AskInput
	registry := NewWithOptions(workspace, RegistryOptions{
		Permission: permissionEvaluatorFunc(func(input permission.AskInput) error {
			request = input
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("permission evaluated after mutation: %v", err)
			}
			return nil
		}),
	})

	// Validation contract A: mutation asks include the proposed diff before I/O.
	if _, err := execute(t, registry, "write", map[string]any{
		"filePath": target, "content": "new content",
	}); err != nil {
		t.Fatal(err)
	}
	diff, _ := request.Metadata["diff"].(string)
	if request.Permission != "edit" || request.Patterns[0] != "proposed.txt" ||
		!strings.Contains(diff, "+new content") {
		t.Fatalf("permission request = %+v", request)
	}
}

func TestEditPermissionDiffNormalizesCRLFContract(t *testing.T) {
	// Parity audit contract 5: edit.ts normalizes CRLF before generating the
	// proposed diff supplied to the permission evaluator.
	workspace := t.TempDir()
	target := filepath.Join(workspace, "windows.txt")
	if err := os.WriteFile(target, []byte("old\r\nkeep\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var request permission.AskInput
	registry := NewWithOptions(workspace, RegistryOptions{
		Permission: permissionEvaluatorFunc(func(input permission.AskInput) error {
			request = input
			return nil
		}),
	})
	if _, err := execute(t, registry, "edit", map[string]any{
		"filePath": target, "oldString": "old", "newString": "new",
	}); err != nil {
		t.Fatal(err)
	}
	diff, _ := request.Metadata["diff"].(string)
	if strings.Contains(diff, "\r") || !strings.Contains(diff, "-old\n+new\n") {
		t.Fatalf("CRLF proposed diff = %q", diff)
	}
}

func TestRegistryExternalDirectoryPermissionFlowContract(t *testing.T) {
	// Parity audit contract: codeaf resolves external targets, asks for their
	// parent glob, then continues through each tool's ordinary permission.
	workspace := t.TempDir()
	external := t.TempDir()
	target := filepath.Join(external, "outside.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requests := []permission.Request{}
	registry := NewWithOptions(workspace, RegistryOptions{
		AllowExternalDirectories: true,
		Permission: permissionEvaluatorFunc(func(input permission.AskInput) error {
			requests = append(requests, input.Request)
			return nil
		}),
	})
	assertPair := func(t *testing.T, ordinary string, run func() error) {
		t.Helper()
		requests = nil
		if err := run(); err != nil {
			t.Fatal(err)
		}
		if len(requests) < 2 || requests[0].Permission != "external_directory" ||
			requests[len(requests)-1].Permission != ordinary ||
			requests[0].Patterns[0] != filepath.ToSlash(filepath.Join(external, "*")) {
			t.Fatalf("%s permission flow = %#v", ordinary, requests)
		}
	}
	assertPair(t, "read", func() error {
		_, err := execute(t, registry, "read", map[string]any{"filePath": target})
		return err
	})
	assertPair(t, "edit", func() error {
		_, err := execute(t, registry, "write", map[string]any{"filePath": target, "content": "write\n"})
		return err
	})
	assertPair(t, "edit", func() error {
		_, err := execute(t, registry, "edit", map[string]any{
			"filePath": target, "oldString": "write", "newString": "edited",
		})
		return err
	})
	assertPair(t, "edit", func() error {
		_, err := execute(t, registry, "apply_patch", map[string]any{
			"patchText": "*** Begin Patch\n*** Update File: " + target + "\n@@\n-edited\n+patched\n*** End Patch",
		})
		return err
	})
	assertPair(t, "bash", func() error {
		_, err := execute(t, registry, "bash", map[string]any{
			"command": "pwd", "workdir": external,
		})
		return err
	})

	confined := New(workspace)
	if _, err := execute(t, confined, "read", map[string]any{"filePath": target}); err == nil || !strings.Contains(err.Error(), "path escapes workspace") {
		t.Fatalf("swedog/default confinement error = %v", err)
	}
}

func TestRegistryHardShellConfinementContract(t *testing.T) {
	// Parity audit contract 2: swedog's registry option rejects parsed shell
	// paths outside the workspace before the autonomous permission evaluator.
	workspace := t.TempDir()
	inside := filepath.Join(workspace, "inside.txt")
	if err := os.WriteFile(inside, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	copyTarget := filepath.Join(external, "copied.txt")
	registry := NewWithOptions(workspace, RegistryOptions{HardConfineShellPaths: true})
	for _, command := range []string{
		"cat /etc/passwd",
		"cp " + inside + " " + copyTarget,
	} {
		if _, err := execute(t, registry, "bash", map[string]any{"command": command}); err == nil ||
			!strings.Contains(err.Error(), "path escapes workspace") {
			t.Fatalf("hard-confined command %q error = %v", command, err)
		}
	}
	if _, err := os.Stat(copyTarget); !os.IsNotExist(err) {
		t.Fatalf("hard-confined cp wrote outside workspace: %v", err)
	}
}

func TestRegistryLivePlanDBGuardsBindMutationAndVerification(t *testing.T) {
	plandb.ResetPlanDBForTesting()
	plandb.RunPlanDB([]string{"plandb", "init", "demo"})
	root := plandb.RunPlanDB([]string{
		"plandb", "add", "root", "--json", "--project", "demo",
		"--description", "Root issue body",
	})
	rootObject := parsePlanJSON(root.Stdout)
	rootID, _ := rootObject["id"].(string)
	projectID, _ := rootObject["project_id"].(string)
	child := plandb.RunPlanDB([]string{
		"plandb", "add", "assigned", "--json", "--project", projectID,
		"--parent", rootID, "--kind", "code",
		"--description", "task_role: implementation\naccess: write\nfile_scope: old.go",
	})
	childID, _ := parsePlanJSON(child.Stdout)["id"].(string)

	workspace := t.TempDir()
	registry := New(workspace)
	instance := project.InstanceContext{
		Directory: workspace, Worktree: workspace,
		Project: project.Info{ID: project.ID(projectID), Worktree: workspace},
	}
	message := func(text string) msgmodel.WithParts {
		return msgmodel.WithParts{
			Info: msgmodel.User{MessageBase: msgmodel.MessageBase{ID: "msg", SessionID: "ses"}},
			Parts: msgmodel.Parts{msgmodel.TextPart{
				PartBase: msgmodel.PartBase{ID: "part", SessionID: "ses", MessageID: "msg"},
				Text:     text,
			}},
		}
	}
	mutationText := "Project: " + projectID + "\nRoot task: " + rootID +
		"\nAssigned PlanDB task: " + childID
	mutationCtx := project.WithContext(context.Background(), instance)
	mutationCtx = steploop.WithToolMessages(mutationCtx, []msgmodel.WithParts{message(mutationText)})
	writeInput := registryTestJSON(t, map[string]any{
		"filePath": filepath.Join(workspace, "new.go"), "content": "package sample\n",
	})
	if _, err := registry.Execute(mutationCtx, steploop.ToolCall{
		Name: "write", Input: writeInput, SessionID: "ses-write", MessageID: "msg-write", Agent: "coder",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(mutationCtx, steploop.ToolCall{
		Name: "edit", Input: registryTestJSON(t, map[string]any{
			"filePath":  filepath.Join(workspace, "new.go"),
			"oldString": "package sample", "newString": "package changed",
		}), SessionID: "ses-edit", MessageID: "msg-edit", Agent: "coder",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(mutationCtx, steploop.ToolCall{
		Name: "apply_patch", Input: registryTestJSON(t, map[string]any{
			"patchText": "*** Begin Patch\n*** Add File: patch.go\n+package patch\n*** End Patch",
		}), SessionID: "ses-patch", MessageID: "msg-patch", Agent: "coder",
	}); err != nil {
		t.Fatal(err)
	}

	// Validation contract B: an assigned package expands to cover an out-of-scope mutation.
	updated := plandb.RunPlanDB([]string{"plandb", "show", childID, "--json"})
	description, _ := parsePlanJSON(updated.Stdout)["description"].(string)
	if !strings.Contains(description, "file_scope: old.go,new.go,patch.go") {
		t.Fatalf("assigned package scope was not expanded: %q", description)
	}

	verificationCtx := project.WithContext(context.Background(), instance)
	verificationCtx = steploop.WithToolMessages(verificationCtx, []msgmodel.WithParts{message(
		"Project: " + projectID + "\nRoot task: " + rootID,
	)})
	if _, err := registry.Execute(verificationCtx, steploop.ToolCall{
		Name: "bash", Input: registryTestJSON(t, map[string]any{"command": "echo 'go test ./...'"}),
		SessionID: "ses-verify", MessageID: "msg-verify", Agent: "auditor",
	}); err != nil {
		t.Fatal(err)
	}

	// Validation contract B: a live verification command records a QA package.
	list := plandb.RunPlanDB([]string{"plandb", "list", "--json", "--project", projectID})
	if !strings.Contains(string(list.Stdout), "Direct verification: echo 'go test ./...'") {
		t.Fatalf("verification package not recorded: %s", list.Stdout)
	}
}
