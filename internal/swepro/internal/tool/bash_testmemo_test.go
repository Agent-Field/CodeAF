package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
)

func TestBashMemoizesIdenticalTestCommandContract(t *testing.T) {
	// Validation contract C7.4: an identical test command against an unchanged
	// tree is served from the per-run memo instead of executing twice.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	workspace := t.TempDir()
	marker := filepath.Join(t.TempDir(), "executions")
	registry := New(workspace)
	command := "printf x >> " + shellQuote(marker) + "; printf 'go test passed'"
	input, err := json.Marshal(bashInput{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	call := steploop.ToolCall{Name: "bash", Input: input, SessionID: "session"}
	first, err := registry.Execute(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Execute(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "x" {
		t.Fatalf("command executions = %q, want one", body)
	}
	if strings.Contains(first.Output, "[codeaf: cached") || !strings.Contains(second.Output, "[codeaf: cached") {
		t.Fatalf("first output = %q; second output = %q", first.Output, second.Output)
	}
}

func TestBashMemoInvalidatesWhenAlreadyDirtyTrackedContentChanges(t *testing.T) {
	// Validation contract C7.3: status porcelain keeps saying
	// "M state.txt", but both green-to-red and red-to-green edits must execute.
	for _, test := range []struct {
		name       string
		first      string
		second     string
		wantFirst  int
		wantSecond int
	}{
		{name: "green-to-red", first: "green\n", second: "red\n", wantFirst: 0, wantSecond: 1},
		{name: "red-to-green", first: "red\n", second: "green\n", wantFirst: 1, wantSecond: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
			workspace := newMemoGitWorkspace(t)
			marker := filepath.Join(t.TempDir(), "executions")
			state := filepath.Join(workspace, "state.txt")
			if err := os.WriteFile(state, []byte(test.first), 0o600); err != nil {
				t.Fatal(err)
			}
			registry := New(workspace)
			command := "printf x >> " + shellQuote(marker) +
				"; grep -q green state.txt; result=$?; printf 'go test observed'; exit $result"
			first := executeMemoBash(t, registry, command)
			if got := bashExitCode(t, first); got != test.wantFirst {
				t.Fatalf("first exit = %d, want %d", got, test.wantFirst)
			}
			if err := os.WriteFile(state, []byte(test.second), 0o600); err != nil {
				t.Fatal(err)
			}
			second := executeMemoBash(t, registry, command)
			if got := bashExitCode(t, second); got != test.wantSecond {
				t.Fatalf("second exit = %d, want %d; output = %q", got, test.wantSecond, second.Output)
			}
			body, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "xx" || strings.Contains(second.Output, "[codeaf: cached") {
				t.Fatalf("executions = %q; second output = %q", body, second.Output)
			}
		})
	}
}

func TestBashMemoSeparatesWorktreesWithSameHeadAndStatusShape(t *testing.T) {
	// Validation contract C7.3: leaf worktrees can share a runtime memo, HEAD,
	// command, and porcelain shape without sharing process observations.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	firstWorkspace := newMemoGitWorkspace(t)
	secondWorkspace := filepath.Join(t.TempDir(), "linked")
	gitMemoRun(t, firstWorkspace, "worktree", "add", "--detach", secondWorkspace, "HEAD")
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", firstWorkspace, "worktree", "remove", "--force", secondWorkspace).Run()
	})
	if err := os.WriteFile(filepath.Join(firstWorkspace, "state.txt"), []byte("green\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondWorkspace, "state.txt"), []byte("red\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstRegistry := New(firstWorkspace)
	secondRegistry := New(secondWorkspace)
	secondRegistry.testMemo = firstRegistry.testMemo
	secondRegistry.testMemoMu = firstRegistry.testMemoMu
	command := "grep -q green state.txt; result=$?; printf 'go test observed'; exit $result"
	if got := bashExitCode(t, executeMemoBash(t, firstRegistry, command)); got != 0 {
		t.Fatalf("first worktree exit = %d, want 0", got)
	}
	second := executeMemoBash(t, secondRegistry, command)
	if got := bashExitCode(t, second); got != 1 || strings.Contains(second.Output, "[codeaf: cached") {
		t.Fatalf("second worktree exit = %d, output = %q", got, second.Output)
	}
}

func TestBashMemoSkipsOversizedDirtyFingerprint(t *testing.T) {
	// Validation contracts C7.1 and C7.2: one dirty file beyond the byte
	// budget makes the entire fingerprint unsafe. The command still executes,
	// and no truncated key is served or stored.
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "1")
	workspace := newMemoGitWorkspace(t)
	largePath := filepath.Join(workspace, "large.bin")
	if err := os.WriteFile(largePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(largePath, testMemoFingerprintMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	status := append([]byte("?? large.bin"), 0)
	if _, err := hashGitDirtyTree(workspace, status); !errors.Is(err, errTestMemoFingerprintBudget) {
		t.Fatalf("oversized fingerprint error = %v, want budget exhaustion", err)
	}

	marker := filepath.Join(t.TempDir(), "executions")
	registry := New(workspace)
	command := "printf x >> " + shellQuote(marker) + "; printf 'go test passed'"
	first := executeMemoBash(t, registry, command)
	second := executeMemoBash(t, registry, command)
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "xx" || strings.Contains(first.Output, "[codeaf: cached") ||
		strings.Contains(second.Output, "[codeaf: cached") {
		t.Fatalf("executions=%q first=%q second=%q", body, first.Output, second.Output)
	}
}

func TestBashMemoPostCheckDoesNotRehashUnchangedContent(t *testing.T) {
	// Validation contract C7.5: after a miss, unchanged metadata proves that
	// the bounded content fingerprint computed for lookup is still current.
	// The post-command guard therefore need not stream the dirty bytes again.
	workspace := newMemoGitWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "state.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, state, safe := bashTestMemoKey(context.Background(), workspace, "go test ./...")
	if !safe || key == "" || state == nil {
		t.Fatal("pre-command fingerprint unexpectedly unsafe")
	}
	postKey, postSafe, rehashed := bashTestMemoPostKey(context.Background(), state)
	if !postSafe || postKey != key || rehashed {
		t.Fatalf("post key=%q safe=%v rehashed=%v, want reused key", postKey, postSafe, rehashed)
	}
}

func newMemoGitWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	gitMemoRun(t, workspace, "init")
	gitMemoRun(t, workspace, "config", "user.email", "memo@example.test")
	gitMemoRun(t, workspace, "config", "user.name", "Memo Test")
	if err := os.WriteFile(filepath.Join(workspace, "state.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitMemoRun(t, workspace, "add", "state.txt")
	gitMemoRun(t, workspace, "commit", "-m", "base")
	return workspace
}

func gitMemoRun(t *testing.T, workspace string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", workspace}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func executeMemoBash(t *testing.T, registry *Registry, command string) steploop.ToolResult {
	t.Helper()
	input, err := json.Marshal(bashInput{Command: command})
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(context.Background(), steploop.ToolCall{
		Name: "bash", Input: input, SessionID: "session",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func bashExitCode(t *testing.T, result steploop.ToolResult) int {
	t.Helper()
	var metadata struct {
		ExitCode int `json:"exitCode"`
	}
	if err := json.Unmarshal(result.Metadata.Raw(), &metadata); err != nil {
		t.Fatal(err)
	}
	return metadata.ExitCode
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
