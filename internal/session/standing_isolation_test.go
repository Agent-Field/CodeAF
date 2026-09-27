package session

import (
	"context"
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

	branchesOut := gitOut(t, repo, "branch", "--list", "standing/*")
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

func TestStandingFalselyClaimingBranchIsolationCaught(t *testing.T) {
	repo := newTestRepo(t)
	hostBranch := currentBranch(repo)
	if hostBranch == "" {
		hostBranch = "work"
	}
	hostHeadBefore := branchCommit(repo, hostBranch)

	item := nightly(repo)
	item.Grant = "" // ambient execution in workspace

	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "bash", `{"command":"git -c user.name=t -c user.email=t@t commit --allow-empty -m \"commit to host\""}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			text := "committed on branch vet-fix, not merged"
			provider.Emit(ctx, provider.StreamDelta, text)
			return textResponse(text), nil
		},
	}}

	root := t.TempDir()
	runDir := filepath.Join(root, "run-2")
	outcome, err := standingChildRunner(t, root, completer).Run(context.Background(), item, runDir, "")
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	hostHeadAfter := branchCommit(repo, hostBranch)
	if hostHeadAfter == hostHeadBefore {
		t.Fatalf("expected host HEAD to move in this test")
	}

	if outcome.Kind == "landed" {
		t.Fatalf("expected outcome NOT to say 'landed' on a lie, got %q", outcome.Kind)
	}
	if outcome.Kind != standing.OutcomeFailed {
		t.Fatalf("expected outcome %q, got %q", standing.OutcomeFailed, outcome.Kind)
	}
	if !strings.Contains(outcome.Text, "branch isolation breach") {
		t.Fatalf("expected outcome.Text to report branch isolation breach, got %q", outcome.Text)
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
