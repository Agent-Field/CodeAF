package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

// A model's own commit uses the run identity, and finishing an otherwise clean
// branch cannot rewrite either that commit or the base it started from.
func TestProgramModelCommitKeepsRunIdentityAndCommitObject(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "config", "user.name", "Person")
	mustGit(t, repo, "config", "user.email", "person@example.test")
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	before := gitOut(t, repo, "show", "-s", "--format=%an <%ae>|%cn <%ce>|%B", base)
	folder, err := PrepareProgramFolder(ProgramFolderOrder{
		Program: testPrograms("fake")[0], Dir: repo, Title: "Model's work",
		Holder: "task 9 (Model's work)", Keep: t.TempDir(), SignModel: "z-ai/glm-5.3-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(folder.release)
	registry := tool.New(repo)
	defer registry.CloseShellProcesses()
	input, _ := json.Marshal(map[string]any{"command": "printf 'model work\\n' > model.txt && git add model.txt && git commit -m 'model work'"})
	result, err := registry.Execute(context.Background(), steploop.ToolCall{ID: "model-commit", Name: "bash", Input: input})
	if err != nil {
		t.Fatalf("model bash commit: %v, %+v", err, result)
	}
	tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	if tip == base {
		t.Fatalf("the model command made no commit: %s", result.Output)
	}
	identity := strings.TrimSpace(gitOut(t, repo, "show", "-s", "--format=%an <%ae>|%cn <%ce>", tip))
	wantIdentity := codeafGitName + " <" + codeafGitEmail + ">|" + codeafGitName + " <" + codeafGitEmail + ">"
	if identity != wantIdentity {
		t.Fatalf("model commit identity = %q, want %q", identity, wantIdentity)
	}
	tree := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD^{tree}"))
	object := gitOut(t, repo, "cat-file", "-p", "HEAD")
	folder.Finish("completed the change")
	if after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); after != tip {
		t.Fatalf("finishing rewrote the model's commit: %s -> %s", tip, after)
	}
	if after := gitOut(t, repo, "cat-file", "-p", "HEAD"); after != object {
		t.Fatal("finishing changed the model's commit object")
	}
	if after := gitOut(t, repo, "show", "-s", "--format=%an <%ae>|%cn <%ce>|%B", base); after != before {
		t.Fatalf("pre-run commit changed:\nbefore %q\nafter %q", before, after)
	}
	if after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD^{tree}")); after != tree {
		t.Fatalf("finishing credit changed the model's tree from %s to %s", tree, after)
	}
	if _, err := git(repo, "merge-base", "--is-ancestor", base, "HEAD"); err != nil {
		t.Fatal("finishing credit lost the person's pre-run commit from history")
	}
}

// A run with nothing to commit cannot attach its credit to the person's
// commit that was already at the tip when the run began.
func TestProgramFinishDoesNotAmendThePreRunTip(t *testing.T) {
	repo := newTestRepo(t)
	base := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	folder, err := PrepareProgramFolder(ProgramFolderOrder{
		Program: testPrograms("fake")[0], Dir: repo, Title: "No change",
		Holder: "task 9 (No change)", Keep: t.TempDir(), SignModel: "z-ai/glm-5.3-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(folder.release)
	folder.Finish("no change")
	if tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); tip != base {
		t.Fatalf("finishing an empty run amended the pre-run tip: %s → %s", base, tip)
	}
}

// An engine checkpoint at the tip is already a commit. With nothing left to
// stage, finishing cannot rewrite it just to add model attribution.
func TestProgramFinishLeavesTheEnginesOwnCheckpointAtTheTip(t *testing.T) {
	repo := newTestRepo(t)
	folder, err := PrepareProgramFolder(ProgramFolderOrder{
		Program: testPrograms("fake")[0], Dir: repo, Title: "Engine work",
		Holder: "task 9", Keep: t.TempDir(), SignModel: "fixture/vendor-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(folder.release)
	if err := os.WriteFile(filepath.Join(repo, "engine.txt"), []byte("engine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "engine.txt")
	mustGit(t, repo, "-c", "user.name="+gitidentity.EngineName, "-c", "user.email="+gitidentity.EngineEmail,
		"commit", "-q", "--no-verify", "-m", "wip(write): engine.txt")
	tip := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	object := gitOut(t, repo, "cat-file", "-p", "HEAD")
	folder.Finish("done")
	if after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); after != tip {
		t.Fatalf("the engine's checkpoint was rewritten: %s -> %s", tip, after)
	}
	if after := gitOut(t, repo, "cat-file", "-p", "HEAD"); after != object {
		t.Fatal("the engine's checkpoint object changed")
	}
}

// A tip the model already pushed is left as it is: amending it would leave the
// person's local branch diverged from the remote for the sake of a trailer.
func TestProgramFinishLeavesAPushedTipAlone(t *testing.T) {
	repo := newTestRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("bare remote: %v: %s", err, out)
	}
	mustGit(t, repo, "remote", "add", "origin", remote)
	folder, err := PrepareProgramFolder(ProgramFolderOrder{
		Program: testPrograms("fake")[0], Dir: repo, Title: "Model work",
		Holder: "task 9", Keep: t.TempDir(), SignModel: "fixture/vendor-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(folder.release)
	registry := tool.New(repo)
	defer registry.CloseShellProcesses()
	input, _ := json.Marshal(map[string]any{"command": "printf 'model work\\n' > model.txt && git add model.txt && git commit -q -m 'model work' && git push -q origin HEAD:refs/heads/task"})
	if result, err := registry.Execute(context.Background(), steploop.ToolCall{ID: "commit", Name: "bash", Input: input}); err != nil {
		t.Fatalf("model commit and push: %v: %+v", err, result)
	}
	mustGit(t, repo, "fetch", "-q", "origin")
	pushed := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	folder.Finish("done")
	if local := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); local != pushed {
		t.Fatalf("finishing rewrote a commit the remote already holds: pushed %s, local now %s", pushed, local)
	}
}

// A commit the person made on the run's branch while it worked is theirs, and
// the finish never rewrites it.
func TestProgramFinishLeavesThePersonsCommitOnTheRunBranchAlone(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "config", "user.name", "Fixture Person")
	mustGit(t, repo, "config", "user.email", "person@example.test")
	folder, err := PrepareProgramFolder(ProgramFolderOrder{
		Program: testPrograms("fake")[0], Dir: repo, Title: "Model work",
		Holder: "task 9", Keep: t.TempDir(), SignModel: "fixture/vendor-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(folder.release)
	if err := os.WriteFile(filepath.Join(repo, "person.txt"), []byte("person\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "person.txt")
	mustGit(t, repo, "commit", "-q", "-m", "person midrun")
	before := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	folder.Finish("done")
	if after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); after != before {
		t.Fatalf("the person's commit on the run branch was amended: %s -> %s", before, after)
	}
}
