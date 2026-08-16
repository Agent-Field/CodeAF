package mergerecovery

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/attribution"
)

func recoveryInput(t *testing.T) Input {
	t.Helper()
	// Pin the committer identity to its default so a developer who exports
	// SWE_AF_GIT_NAME/EMAIL in their shell does not fail the argv assertions.
	t.Setenv(attribution.EnvCommitterName, "")
	t.Setenv(attribution.EnvCommitterEmail, "")
	root := t.TempDir()
	leaf := filepath.Join(root, "leaf")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	return Input{
		Workspace: root, Worktree: leaf, MergeAt: target,
		TaskID: "17", TaskTitle: "resolve it", TargetBranch: "main",
		SourceBranch: "plandb/17",
		PriorFailure: PriorFailure{Summary: "rebase failed"},
	}
}

func executeTool(t *testing.T, request Request, name string, arg any) string {
	t.Helper()
	tool, ok := request.Tool(name)
	if !ok {
		t.Fatalf("missing tool %q", name)
	}
	result, err := tool.Execute(arg)
	if err != nil {
		t.Fatalf("%s tool: %v", name, err)
	}
	return result
}

func TestRecoveryToolsAndSuccessfulReport(t *testing.T) {
	input := recoveryInput(t)
	if err := os.WriteFile(filepath.Join(input.Worktree, "conflict.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotArgv [][]string
	var gotCWD []string
	input.Runner = RunnerFunc(func(argv []string, cwd string) (ProcessResult, error) {
		gotArgv = append(gotArgv, append([]string{}, argv...))
		gotCWD = append(gotCWD, cwd)
		return ProcessResult{Code: 3, Stdout: []byte("out"), Stderr: []byte("err")}, nil
	})
	input.Client = ClientFunc(func(request Request) error {
		if request.Prompt != BuildPlaybook(input) || request.MaxSteps != 24 ||
			request.Temperature != 0.1 || request.MaxRetries != 0 ||
			len(request.Tools) != 5 {
			t.Fatalf("request = %#v", request)
		}
		if got := executeTool(t, request, "git_leaf", map[string]any{
			"argv": []any{"git", "rebase", "main"},
		}); got != "exit=3\nstdout:\nout\nstderr:\nerr" {
			t.Fatalf("git leaf output = %q", got)
		}
		if got := executeTool(t, request, "git_target", map[string]any{
			"argv": []any{"git", "merge", "--no-ff", "plandb/17"},
		}); got != "exit=3\nstdout:\nout\nstderr:\nerr" {
			t.Fatalf("git target output = %q", got)
		}
		if got := executeTool(t, request, "read", map[string]any{
			"path": "conflict.txt",
		}); got != "before\n" {
			t.Fatalf("read output = %q", got)
		}
		if got := executeTool(t, request, "write", map[string]any{
			"path": "nested/resolved.txt", "content": "😀",
		}); got != "ok: wrote 2 bytes" {
			t.Fatalf("write output = %q", got)
		}
		if got := executeTool(t, request, "report", map[string]any{
			"ok": true, "summary": "landed cleanly",
		}); got != "verdict recorded; stop now" {
			t.Fatalf("report output = %q", got)
		}
		return nil
	})

	result := LLMMergeRecovery(input)
	if result != (Result{
		OK: true, Summary: "landed cleanly", ToolCalls: 4,
	}) {
		t.Fatalf("result = %#v", result)
	}
	wantLeaf := []string{
		"git", "-c", "user.name=SWE-AF",
		"-c", "user.email=swe-af@users.noreply.github.com", "rebase", "main",
	}
	wantTarget := []string{
		"git", "-c", "user.name=SWE-AF",
		"-c", "user.email=swe-af@users.noreply.github.com",
		"merge", "--no-ff", "plandb/17",
	}
	if !reflect.DeepEqual(gotArgv, [][]string{wantLeaf, wantTarget}) ||
		!reflect.DeepEqual(gotCWD, []string{
			resolvePath(input.Worktree), resolvePath(input.MergeAt),
		}) {
		t.Fatalf("argv=%#v cwd=%#v", gotArgv, gotCWD)
	}
	content, err := os.ReadFile(filepath.Join(input.Worktree, "nested", "resolved.txt"))
	if err != nil || string(content) != "😀" {
		t.Fatalf("written content=%q err=%v", content, err)
	}
}

func TestGitToolsAppendCommitAttribution(t *testing.T) {
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "")
	input := recoveryInput(t)
	var gotArgv [][]string
	input.Runner = RunnerFunc(func(argv []string, cwd string) (ProcessResult, error) {
		gotArgv = append(gotArgv, append([]string{}, argv...))
		return ProcessResult{Code: 0}, nil
	})
	input.Client = ClientFunc(func(request Request) error {
		executeTool(t, request, "git_leaf", map[string]any{
			"argv": []any{"git", "commit", "-m", "leaf 17: resolve it"},
		})
		executeTool(t, request, "git_leaf", map[string]any{
			"argv": []any{"git", "rebase", "main"},
		})
		executeTool(t, request, "git_target", map[string]any{
			"argv": []any{"git", "merge", "--no-ff", "plandb/17", "-m", "leaf 17 → main"},
		})
		return nil
	})
	result := LLMMergeRecovery(input)
	if result.ToolCalls != 3 {
		t.Fatalf("result = %#v", result)
	}
	if len(gotArgv) != 3 {
		t.Fatalf("runner saw %d calls: %v", len(gotArgv), gotArgv)
	}
	commit, rebase, merge := gotArgv[0], gotArgv[1], gotArgv[2]
	if got := commit[len(commit)-1]; !strings.Contains(got, "Co-Authored-By: SWE AF") ||
		!strings.HasPrefix(got, "leaf 17: resolve it\n\n") {
		t.Errorf("git_leaf commit message = %q", got)
	}
	if commit[1] != "-c" {
		t.Errorf("committer flags not injected first: %v", commit)
	}
	for _, arg := range rebase {
		if strings.Contains(arg, "Co-Authored-By") {
			t.Errorf("rebase argv rewritten: %v", rebase)
		}
	}
	if got := merge[len(merge)-1]; !strings.Contains(got, "Co-Authored-By: SWE AF") {
		t.Errorf("git_target merge message = %q", got)
	}
}

func TestToolValidationAndMarkerQuirk(t *testing.T) {
	input := recoveryInput(t)
	input.Client = ClientFunc(func(request Request) error {
		if got := executeTool(t, request, "git_leaf", map[string]any{
			"argv": "git status",
		}); got != "error: argv[0] must be 'git'" {
			t.Fatalf("invalid argv = %q", got)
		}
		outside := filepath.Join(filepath.Dir(input.Worktree), "outside.txt")
		if got := executeTool(t, request, "read", map[string]any{
			"path": outside,
		}); got != "error: path "+outside+" is outside the leaf worktree" {
			t.Fatalf("outside read = %q", got)
		}
		if got := executeTool(t, request, "write", map[string]any{
			"path": "bad.txt", "content": "<<<<<<< ours\nx",
		}); got != "error: content still contains conflict markers" {
			t.Fatalf("opening marker = %q", got)
		}
		if got := executeTool(t, request, "write", map[string]any{
			"path": "kept.txt", "content": "=======\nx\n>>>>>>> theirs",
		}); got != "ok: wrote 24 bytes" {
			t.Fatalf("other markers = %q", got)
		}
		executeTool(t, request, "report", map[string]any{
			"ok": false, "summary": "stuck", "conflict_path": "/tmp/report.md",
		})
		return nil
	})
	result := LLMMergeRecovery(input)
	if result.OK || result.Summary != "stuck" ||
		result.ConflictPath != "/tmp/report.md" || result.ToolCalls != 4 {
		t.Fatalf("result = %#v", result)
	}
}

func TestLexicalPathGuardKeepsSourceSymlinkEscape(t *testing.T) {
	input := recoveryInput(t)
	outside := filepath.Join(filepath.Dir(input.Worktree), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(input.Worktree), filepath.Join(input.Worktree, "escape")); err != nil {
		t.Fatal(err)
	}
	input.Client = ClientFunc(func(request Request) error {
		got := executeTool(t, request, "read", map[string]any{
			"path": "escape/outside.txt",
		})
		if got != "outside secret" {
			t.Fatalf("symlink read = %q", got)
		}
		executeTool(t, request, "report", map[string]any{
			"ok": true, "summary": "observed source behavior",
		})
		return nil
	})
	result := LLMMergeRecovery(input)
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestRecoveryDegradationBranches(t *testing.T) {
	t.Run("missing report", func(t *testing.T) {
		input := recoveryInput(t)
		zero := 0.0
		input.MaxSteps = &zero
		input.Client = ClientFunc(func(request Request) error {
			if got := executeTool(t, request, "read", map[string]any{
				"path": "missing",
			}); !strings.HasPrefix(got, "error: ENOENT: no such file or directory, open '") {
				t.Fatalf("read error = %q", got)
			}
			return nil
		})
		result := LLMMergeRecovery(input)
		if result != (Result{
			OK:        false,
			Summary:   "recovery did not call report() within 0 steps (last toolCalls=1)",
			ToolCalls: 1,
		}) {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("client error slices without truncate ellipsis", func(t *testing.T) {
		input := recoveryInput(t)
		message := strings.Repeat("x", 250)
		input.Client = ClientFunc(func(Request) error { return errors.New(message) })
		result := LLMMergeRecovery(input)
		want := "recovery stream errored: " + strings.Repeat("x", 200)
		if result != (Result{OK: false, Summary: want}) {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("client panic folds into stream error", func(t *testing.T) {
		input := recoveryInput(t)
		input.Client = ClientFunc(func(Request) error { panic("boom") })
		result := LLMMergeRecovery(input)
		if result.Summary != "recovery stream errored: boom" {
			t.Fatalf("result = %#v", result)
		}
	})
}

func TestGitOutputTruncationUsesUTF16(t *testing.T) {
	input := recoveryInput(t)
	input.Runner = RunnerFunc(func([]string, string) (ProcessResult, error) {
		return ProcessResult{Stdout: []byte(strings.Repeat("😀", 2001) + "tail")}, nil
	})
	input.Client = ClientFunc(func(request Request) error {
		got := executeTool(t, request, "git_leaf", map[string]any{
			"argv": []any{"git", "status"},
		})
		want := "exit=0\nstdout:\n" + strings.Repeat("😀", 1998) +
			"�...\nstderr:\n"
		if got != want {
			t.Fatalf("unexpected truncated output length=%d suffix=%q", len(got), got[len(got)-40:])
		}
		executeTool(t, request, "report", map[string]any{
			"ok": true, "summary": "done",
		})
		return nil
	})
	if result := LLMMergeRecovery(input); !result.OK {
		t.Fatalf("result = %#v", result)
	}
}
