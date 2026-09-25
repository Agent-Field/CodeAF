package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

// A command the model writes and the finishing hand must give the same run
// credit, without rewriting a commit that was present before the run.
func TestProgramModelCommitHasRunIdentityAndFinishingCredit(t *testing.T) {
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
	folder.Finish("completed the change")
	message := gitOut(t, repo, "show", "-s", "--format=%B", "HEAD")
	if !strings.Contains(message, "Assisted-by:") || !strings.Contains(message, "glm-5.3-flash") {
		t.Fatalf("model commit lacks answered-model credit: %q", message)
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
