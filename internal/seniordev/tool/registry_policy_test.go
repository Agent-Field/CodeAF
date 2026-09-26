//go:build !windows

package tool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/permission"
)

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

	// A deny rule rejects before any mutation happens.
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

	// An unattended literal ask is auto-approved.
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

	// Mutation asks include the proposed diff before any I/O.
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
	// The proposed diff handed to the permission evaluator is CRLF-normalized.
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
	// senior-dev resolves external targets, asks for their parent glob, then
	// continues through each tool's ordinary permission.
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
		t.Fatalf("default confinement error = %v", err)
	}
}

func TestRegistryHardShellConfinementContract(t *testing.T) {
	// HardConfineShellPaths rejects parsed shell paths outside the workspace
	// before the permission evaluator is consulted.
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
