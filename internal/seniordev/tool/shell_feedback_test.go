//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

func TestShellSessionBuildCachesAreIsolated(t *testing.T) {
	// Concurrent leaves receive distinct build-cache namespaces.
	for _, name := range []string{
		"CARGO_TARGET_DIR", "GOCACHE", "GOMODCACHE", "npm_config_cache", "PIP_CACHE_DIR",
	} {
		unsetEnvironmentForTest(t, name)
	}
	root := t.TempDir()
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", root)
	t.Setenv("SENIOR_DEV_SHARED_BUILD_CACHE", "0")
	first := environmentMap(shellEnvironment("ses_first"))
	second := environmentMap(shellEnvironment("ses_second"))
	for name, suffix := range map[string]string{
		"CARGO_TARGET_DIR": "cargo", "GOCACHE": "go-build",
		"npm_config_cache": "npm", "PIP_CACHE_DIR": "pip",
	} {
		wantFirst := filepath.Join(root, "ses_first", suffix)
		wantSecond := filepath.Join(root, "ses_second", suffix)
		if first[name] != wantFirst || second[name] != wantSecond || first[name] == second[name] {
			t.Fatalf("%s paths = %q, %q", name, first[name], second[name])
		}
	}
}

func TestShellScratchLeavesGOMODCACHEAlone(t *testing.T) {
	// GOMODCACHE is a source of truth, not a derived cache: the module sources
	// live in it, and an offline environment may have pre-populated it.
	// Redirecting it to an empty per-session dir while the network is
	// blackholed leaves Go unable to build. It must be inherited, never
	// rewritten.
	for _, name := range []string{"GOMODCACHE", "GOCACHE"} {
		unsetEnvironmentForTest(t, name)
	}
	root := t.TempDir()
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", root)
	t.Setenv("SENIOR_DEV_SHARED_BUILD_CACHE", "0")
	environment := environmentMap(shellEnvironment("ses_gomod"))
	if value, set := environment["GOMODCACHE"]; set {
		t.Fatalf("GOMODCACHE was redirected to %q; it must be inherited untouched", value)
	}
	// The derived cache next to it still is redirected, proving the isolation
	// mechanism is intact and only the source-of-truth entry was removed.
	if want := filepath.Join(root, "ses_gomod", "go-build"); environment["GOCACHE"] != want {
		t.Fatalf("GOCACHE = %q, want %q", environment["GOCACHE"], want)
	}
}

func TestShellScratchHonoursInheritedGOMODCACHE(t *testing.T) {
	// A GOMODCACHE the operator set must survive untouched.
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	t.Setenv("SENIOR_DEV_SHARED_BUILD_CACHE", "0")
	t.Setenv("GOMODCACHE", "/root/go/pkg/mod")
	if got := environmentMap(shellEnvironment("ses_inherit"))["GOMODCACHE"]; got != "/root/go/pkg/mod" {
		t.Fatalf("GOMODCACHE = %q, want the inherited /root/go/pkg/mod", got)
	}
}

func TestShellScratchTeardownAtLeafEndContract(t *testing.T) {
	// A completed leaf reclaims its private caches.
	root := t.TempDir()
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", root)
	t.Setenv("SENIOR_DEV_SHARED_BUILD_CACHE", "0")
	_ = shellEnvironment("ses_finished")
	cache := filepath.Join(root, "ses_finished", "go-build")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	TeardownShellScratch("ses_finished")
	if _, err := os.Stat(filepath.Join(root, "ses_finished")); !os.IsNotExist(err) {
		t.Fatalf("completed leaf scratch remains: %v", err)
	}
}

func TestShellScratchTeardownWaitsForLastSessionUserContract(t *testing.T) {
	// Two concurrent leaves sharing a claimed session cannot delete one
	// another's live build caches.
	root := t.TempDir()
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", root)
	firstRelease := AcquireShellScratch("ses_shared")
	secondRelease := AcquireShellScratch("ses_shared")
	cache := filepath.Join(root, "ses_shared", "go-build")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	firstRelease()
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("first user removed shared scratch: %v", err)
	}
	secondRelease()
	if _, err := os.Stat(filepath.Join(root, "ses_shared")); !os.IsNotExist(err) {
		t.Fatalf("last user did not remove shared scratch: %v", err)
	}
}

func TestBashWorkdirRunsThereAndRejectsEscapes(t *testing.T) {
	// workdir runs inside the workspace and rejects escapes.
	workDir := t.TempDir()
	nested := filepath.Join(workDir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := New(workDir)
	result, err := execute(t, registry, "bash", map[string]any{"command": "pwd", "workdir": "nested"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Output) != nested {
		t.Fatalf("pwd output = %q, want %q", result.Output, nested)
	}
	_, err = execute(t, registry, "bash", map[string]any{"command": "pwd", "workdir": "../outside"})
	if err == nil || err.Error() != "path escapes workspace: ../outside" {
		t.Fatalf("escape error = %v", err)
	}
}

func TestBashHonorsConfiguredShell(t *testing.T) {
	// Shell execution honors the configured acceptable shell.
	workDir := t.TempDir()
	shell := filepath.Join(workDir, "configured-sh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'configured-shell\\n'\nexec /bin/sh \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(map[string]any{"shell": shell})
	t.Setenv("SENIOR_DEV_CONFIG_CONTENT", string(settings))
	result, err := execute(t, New(workDir), "bash", map[string]any{"command": "printf command-body"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "configured-shell\ncommand-body" {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestBashReportsOOMKill(t *testing.T) {
	// A cgroup OOM kill is diagnosed instead of appearing as a plain exit status.
	previousOOM := bashReadOOMCount
	previousLimit := bashReadMemoryLimit
	t.Cleanup(func() {
		bashReadOOMCount = previousOOM
		bashReadMemoryLimit = previousLimit
	})
	reads := 0
	bashReadOOMCount = func() *int64 {
		reads++
		value := int64(8)
		if reads > 1 {
			value = 9
		}
		return &value
	}
	bashReadMemoryLimit = func() *int64 {
		value := int64(2 * 1024 * 1024 * 1024)
		return &value
	}
	t.Setenv("SENIOR_DEV_ENV_SIGNALS", "1")
	input := json.RawMessage(`{"command":"exit 137"}`)
	result, err := New(t.TempDir()).Execute(context.Background(), steploop.ToolCall{
		ID: "call_oom", Name: "bash", Input: input, SessionID: "ses_oom",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "were OOM-killed by the kernel") ||
		!strings.Contains(result.Output, "environment resource limit, not a code bug") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestShellClassifiesStallAndRepeatedFailure(t *testing.T) {
	// Quiet timeouts and repeated heavy/failing commands are actionable.
	exit := 1
	quiet := 45 * time.Second
	stall := classifyShellDeath(shellDeathInput{
		ExitCode: &exit, Expired: true, SinceLastOutput: &quiet, Timeout: time.Minute,
	})
	if !strings.Contains(stall, "it was stalled") || !strings.Contains(stall, "larger timeout alone") {
		t.Fatalf("stall diagnostic = %q", stall)
	}
	session := "ses_repeat_test"
	for attempt := 1; attempt <= 3; attempt++ {
		warning := registerShellOutcome(session, "go test ./... 2>&1 | tail -20", time.Second, true)
		if attempt < 3 && warning != "" {
			t.Fatalf("attempt %d warned early: %q", attempt, warning)
		}
		if attempt == 3 && !strings.Contains(warning, "repeated attempt") {
			t.Fatalf("third attempt warning = %q", warning)
		}
	}
}

func environmentMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, value := range values {
		name, item, _ := strings.Cut(value, "=")
		out[name] = item
	}
	return out
}

func unsetEnvironmentForTest(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}
