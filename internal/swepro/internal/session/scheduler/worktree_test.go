package scheduler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/isolationfurrow"
)

func gitRun(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	gitRun(t, repository, "init")
	gitRun(t, repository, "config", "user.email", "scheduler@example.test")
	gitRun(t, repository, "config", "user.name", "Scheduler Test")
	if err := os.WriteFile(filepath.Join(repository, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	gitRun(t, repository, "add", "tracked.txt")
	gitRun(t, repository, "commit", "-m", "base")
	return repository
}

type recordingHygiene struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingHygiene) EnsureCodeafExcluded(_ context.Context, worktree string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "exclude:"+worktree)
	return nil
}

func (r *recordingHygiene) SuppressCaseCollisions(_ context.Context, worktree string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "collisions:"+worktree)
	return nil
}

func TestAllocateWorktreeCreatesAndReplacesRealGitWorktree(t *testing.T) {
	repository := newGitRepository(t)
	hygiene := &recordingHygiene{}
	options := AllocateWorktreeOptions{hygiene: hygiene}

	first := AllocateWorktree(context.Background(), repository, "leaf-a", "HEAD", options)
	if first == nil {
		t.Fatal("AllocateWorktree returned nil")
	}
	wantPath := filepath.Join(repository, ".plandb", "wt-leaf-a")
	if first.Path != wantPath || first.BaseRef != "HEAD" || first.Backend != WorktreeBackendGit {
		t.Fatalf("allocation = %#v", first)
	}
	if want := gitRun(t, repository, "rev-parse", "HEAD"); first.BaseSHA != want {
		t.Fatalf("base SHA = %q, want %q", first.BaseSHA, want)
	}
	if branch := gitRun(t, first.Path, "branch", "--show-current"); branch != "plandb/leaf-a" {
		t.Fatalf("worktree branch = %q", branch)
	}

	if err := os.WriteFile(filepath.Join(first.Path, "tracked.txt"), []byte("rejected change\n"), 0o644); err != nil {
		t.Fatalf("modify worktree: %v", err)
	}
	second := AllocateWorktree(context.Background(), repository, "leaf-a", "HEAD", options)
	if second == nil || second.Path != first.Path || second.BaseSHA != first.BaseSHA {
		t.Fatalf("replacement allocation = %#v", second)
	}
	content, err := os.ReadFile(filepath.Join(second.Path, "tracked.txt"))
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if string(content) != "base\n" {
		t.Fatalf("replacement retained stale content %q", content)
	}
	if len(hygiene.calls) != 4 {
		t.Fatalf("hygiene calls = %#v", hygiene.calls)
	}
}

type fakeFurrow struct {
	result isolationfurrow.ForkResult
	calls  int
}

func (f *fakeFurrow) Fork(workspace, name string) isolationfurrow.ForkResult {
	f.calls++
	return f.result
}

func TestAllocateWorktreeIsolationSeam(t *testing.T) {
	repository := newGitRepository(t)
	furrow := &fakeFurrow{result: isolationfurrow.ForkResult{Path: "/tmp/furrow-leaf"}}
	requiresMerge := false
	probeCalls := 0
	allocation := AllocateWorktree(
		context.Background(),
		repository,
		"furrow-a",
		"HEAD",
		AllocateWorktreeOptions{
			RequiresMerge: &requiresMerge,
			IsolationEnv:  map[string]string{"CODEAF_ISOLATION": "furrow"},
			CowProbe: func() bool {
				probeCalls++
				return true
			},
			furrowFactory: func() furrowForker { return furrow },
		},
	)
	if allocation == nil || allocation.Backend != WorktreeBackendFurrow || allocation.Path != "/tmp/furrow-leaf" {
		t.Fatalf("furrow allocation = %#v", allocation)
	}
	if furrow.calls != 1 || probeCalls != 1 {
		t.Fatalf("fork/probe calls = %d/%d", furrow.calls, probeCalls)
	}

	// Merge-requiring leaves short-circuit before the COW probe and use git.
	requiresMerge = true
	probeCalls = 0
	furrow.calls = 0
	allocation = AllocateWorktree(
		context.Background(),
		repository,
		"furrow-blocked",
		"HEAD",
		AllocateWorktreeOptions{
			RequiresMerge: &requiresMerge,
			IsolationEnv:  map[string]string{"CODEAF_ISOLATION": "furrow"},
			CowProbe: func() bool {
				probeCalls++
				return true
			},
			furrowFactory: func() furrowForker { return furrow },
		},
	)
	if allocation == nil || allocation.Backend != WorktreeBackendGit {
		t.Fatalf("merge-required allocation = %#v", allocation)
	}
	if furrow.calls != 0 || probeCalls != 0 {
		t.Fatalf("merge-required fork/probe calls = %d/%d", furrow.calls, probeCalls)
	}
}

func TestGCStaleWorktreesWrongDatabaseSweepsAll(t *testing.T) {
	repository := newGitRepository(t)
	for _, taskID := range []string{"gc-a", "gc-b"} {
		if allocation := AllocateWorktree(context.Background(), repository, taskID, "HEAD"); allocation == nil {
			t.Fatalf("allocate %s returned nil", taskID)
		}
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find git: %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(binDir, "git")); err != nil {
		t.Fatalf("symlink git: %v", err)
	}
	t.Setenv("PATH", binDir) // real git remains; the real plandb binary disappears.

	cleaned := gcStaleWorktrees(
		context.Background(),
		repository,
		filepath.Join(repository, "plandb.sqlite"),
		"project",
	)
	if cleaned != 2 {
		t.Fatalf("cleaned = %d, want 2", cleaned)
	}
	for _, taskID := range []string{"gc-a", "gc-b"} {
		if _, err := os.Stat(filepath.Join(repository, ".plandb", "wt-"+taskID)); !os.IsNotExist(err) {
			t.Fatalf("worktree %s still exists: %v", taskID, err)
		}
		cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/plandb/"+taskID)
		cmd.Dir = repository
		if err := cmd.Run(); err == nil {
			t.Fatalf("branch plandb/%s still exists", taskID)
		}
	}
	if again := gcStaleWorktrees(context.Background(), repository, "ignored", "project"); again != 0 {
		t.Fatalf("second sweep = %d, want 0", again)
	}
}

func TestForceSweepReclaimsLiveWorktreesAfterPreserving(t *testing.T) {
	repository := newGitRepository(t)
	taskIDs := []string{"live-commit", "live-empty"}
	allocations := make(map[string]*WorktreeAllocation, len(taskIDs))
	for _, taskID := range taskIDs {
		allocation := AllocateWorktree(context.Background(), repository, taskID, "HEAD")
		if allocation == nil {
			t.Fatalf("allocate %s returned nil", taskID)
		}
		allocations[taskID] = allocation
	}
	committedPath := filepath.Join(allocations["live-commit"].Path, "committed.txt")
	if err := os.WriteFile(committedPath, []byte("preserve this commit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, allocations["live-commit"].Path, "add", "committed.txt")
	gitRun(t, allocations["live-commit"].Path, "commit", "-m", "committed live work")

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find git: %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(binDir, "git")); err != nil {
		t.Fatalf("symlink git: %v", err)
	}
	plandbStub := "#!/bin/sh\nprintf '%s\\n' '[{\"id\":\"live-commit\",\"status\":\"running\"},{\"id\":\"live-empty\",\"status\":\"running\"}]'\n"
	if err := os.WriteFile(filepath.Join(binDir, "plandb"), []byte(plandbStub), 0o755); err != nil {
		t.Fatalf("write plandb stub: %v", err)
	}
	t.Setenv("PATH", binDir)

	if cleaned := gcStaleWorktrees(
		context.Background(), repository, filepath.Join(repository, "plandb.sqlite"), "project",
	); cleaned != 0 {
		t.Fatalf("ordinary GC reclaimed %d running worktrees, want 0", cleaned)
	}
	for _, taskID := range taskIDs {
		if _, err := os.Stat(allocations[taskID].Path); err != nil {
			t.Fatalf("ordinary GC removed live worktree %s: %v", taskID, err)
		}
	}

	cleaned := ForceSweepWorktrees(
		context.Background(), repository, filepath.Join(repository, "plandb.sqlite"), "project",
	)
	if cleaned != 2 {
		t.Fatalf("cleaned = %d, want 2", cleaned)
	}
	for _, taskID := range taskIDs {
		if _, err := os.Stat(allocations[taskID].Path); !os.IsNotExist(err) {
			t.Fatalf("worktree %s still exists: %v", taskID, err)
		}
		cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/plandb/"+taskID)
		cmd.Dir = repository
		if err := cmd.Run(); err == nil {
			t.Fatalf("branch plandb/%s still exists", taskID)
		}
	}
	patchPath := filepath.Join(repository, ".codeaf", "rejected-work", "live-commit.patch")
	patch, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("committed work was removed before preservation: %v", err)
	}
	if !strings.Contains(string(patch), "committed.txt") ||
		!strings.Contains(string(patch), "preserve this commit") {
		t.Fatalf("preserved patch does not contain committed work:\n%s", patch)
	}
}
