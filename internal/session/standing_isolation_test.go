package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/standing"
)

func TestStandingBranchIsolationLeavesHostUntouched(t *testing.T) {
	repo := newTestRepo(t)
	hostBranch := currentBranch(repo)
	if hostBranch == "" {
		hostBranch = "work"
	}
	hostHeadBefore := branchCommit(repo, hostBranch)

	item := nightly(repo)
	item.Grant = "open a pull request, never merge one"
	item.Does.Isolate = true

	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "bash", `{"command":"git -c user.name=t -c user.email=t@t commit --allow-empty -m \"isolated branch commit\""}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			text := "committed on branch, not merged"
			provider.Emit(ctx, provider.StreamDelta, text)
			return textResponse(text), nil
		},
	}}

	root := t.TempDir()
	runDir := filepath.Join(root, "run-1")
	outcome, err := standingChildRunner(t, root, completer).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if outcome.Kind != "landed" {
		t.Fatalf("expected outcome landed, got %q", outcome.Kind)
	}

	hostHeadAfter := branchCommit(repo, hostBranch)
	if hostHeadAfter != hostHeadBefore {
		t.Fatalf("host branch HEAD moved! before=%s after=%s", hostHeadBefore, hostHeadAfter)
	}

	branchesOut := gitOut(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/standing/")
	branchNames := strings.Fields(branchesOut)
	if len(branchNames) == 0 {
		t.Fatalf("expected dedicated standing branch in repo, got none: %q", branchesOut)
	}
	standingBranch := strings.TrimPrefix(branchNames[0], "*")
	standingBranch = strings.TrimSpace(standingBranch)

	branchSha := branchCommit(repo, standingBranch)
	if branchSha == "" || branchSha == hostHeadBefore {
		t.Fatalf("expected dedicated branch commit to exist and differ from hostHeadBefore")
	}

	// Commits must be reachable from standingBranch and NOT reachable from hostBranch.
	if _, err := git(repo, "merge-base", "--is-ancestor", branchSha, hostBranch); err == nil {
		t.Fatalf("branch commit %s must not be reachable from %s", branchSha, hostBranch)
	}
	if _, err := git(repo, "merge-base", "--is-ancestor", branchSha, standingBranch); err != nil {
		t.Fatalf("branch commit %s must be reachable from %s: %v", branchSha, standingBranch, err)
	}
}

// A worker's unfinished files remain in the recorded worktree after Close.
func TestStandingIsolationRetainsUncommittedWork(t *testing.T) {
	repo := newTestRepo(t)
	item := nightly(repo)
	item.Does.Isolate = true
	var workspace string
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "bash", `{"command":"printf unfinished > retained.txt"}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("No pull request was needed."), nil
		},
	}}
	root := t.TempDir()
	runner := standingChildRunner(t, root, completer)
	original := runner.child
	runner.child = func(cfg Config) (*Agent, error) { workspace = cfg.Workspace; return original(cfg) }
	runDir := filepath.Join(root, "run-kept")
	outcome, err := runner.Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if workspace == repo {
		t.Fatal("ran in host checkout")
	}
	data, err := os.ReadFile(filepath.Join(workspace, "retained.txt"))
	if err != nil || string(data) != "unfinished" {
		t.Fatalf("work lost: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "retained.txt")); !os.IsNotExist(err) {
		t.Fatal("host checkout was changed")
	}
	if outcome.Kind == standing.OutcomeFailed {
		t.Fatalf("reply prose changed outcome: %+v", outcome)
	}
	trees := loadStandingTrees(runDir)
	if len(trees) != 1 || trees[0].Dir != workspace {
		t.Fatalf("missing recovery record: %+v", trees)
	}
}

func TestStandingIsolationRefusesNonRepositoryBeforeCallingModel(t *testing.T) {
	item := nightly(t.TempDir())
	item.Does.Isolate = true
	root := t.TempDir()
	runner := standingChildRunner(t, root, &scriptedCompleter{})
	runner.child = func(Config) (*Agent, error) { t.Fatal("opened worker for impossible isolation"); return nil, nil }
	if _, err := runner.Run(context.Background(), item, filepath.Join(root, "run"), ""); err == nil {
		t.Fatal("accepted impossible isolation")
	}
}

func TestStandingAmbientPlainFolderWithoutGrant(t *testing.T) {
	plainDir := t.TempDir()
	writeFile(t, filepath.Join(plainDir, "data.txt"), "sample data\n")

	item := nightly(plainDir)
	item.Grant = ""

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			text := "everything looks fine"
			provider.Emit(ctx, provider.StreamDelta, text)
			return textResponse(text), nil
		},
	}}

	root := t.TempDir()
	runDir := filepath.Join(root, "run-3")
	outcome, err := standingChildRunner(t, root, completer).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if outcome.Kind != "landed" {
		t.Fatalf("expected outcome landed, got %q", outcome.Kind)
	}
	if !strings.Contains(outcome.Text, "everything looks fine") {
		t.Fatalf("expected outcome.Text to contain response, got %q", outcome.Text)
	}
}
