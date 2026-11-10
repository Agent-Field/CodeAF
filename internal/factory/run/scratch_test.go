package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A PROOF RUN LEAVES NO EXTRA FILES IN THE DIFF. The 2026-11-06 live run's
// proof step copied the repository to `.proof-prefix-15992/` inside the
// checkout; the copy became ten files of the change, tripped the protected-path
// approval and was committed into PR #148. Now the step is handed a scratch
// folder outside the checkout, and a copy left in the checkout anyway is not
// measured, judged or committed: the diff is the real change alone.
func TestAProofRunLeavesNoExtraFilesInTheDiff(t *testing.T) {
	dir := gitRig(t)
	for name, body := range map[string]string{"auth/token.go": "package auth\n", "README.md": "# api\n"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "more")
	mustGit(t, dir, "checkout", "-q", "-b", "factory/147")
	base := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD"))

	stages := factory.DefaultRecipe().For(factory.KindIssue)
	proof := stages[factory.StageIndex(stages, "proof")]
	job := Job{Item: factory.Item{ID: 9, Num: 147, Repo: "api", Product: "acme", Title: "add Count", Stages: stages, ChangeBase: base}, Stage: proof, Round: 1, Dir: dir}
	scratch := ScratchDir(dir)
	t.Cleanup(func() { _ = os.RemoveAll(scratch) })
	if scratch == "" || strings.HasPrefix(scratch, dir+string(filepath.Separator)) || scratch == dir {
		t.Fatalf("scratch %q is inside the checkout %q", scratch, dir)
	}
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		if _, err := os.Stat(scratch); err != nil {
			t.Errorf("the scratch folder was not made before the step opened: %v", err)
		}
		// The real change, a note in the scratch folder, and — as the live
		// run did — a whole copy of the repository inside the checkout.
		_ = os.WriteFile(filepath.Join(dir, "ledger.go"), []byte("package ledger\n\nfunc Count() int { return 0 }\n"), 0o600)
		_ = os.WriteFile(filepath.Join(scratch, "prefix.log"), []byte("ok\n"), 0o600)
		for _, name := range []string{"ledger.go", "auth/token.go", "README.md"} {
			src, _ := os.ReadFile(filepath.Join(dir, name))
			to := filepath.Join(dir, ".proof-prefix-15992", name)
			_ = os.MkdirAll(filepath.Dir(to), 0o755)
			_ = os.WriteFile(to, src, 0o600)
		}
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "proven", Claims: []factory.Claim{{Text: "Count works", Evidence: "go test"}}})
	}}
	if _, err := NewChatExecutor(maker).Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if brief := maker.Opened()[0].Spec.Brief; !strings.Contains(brief, "scratch: "+scratch) || !strings.Contains(brief, "Make none of them inside the checkout") {
		t.Fatalf("the brief does not hand the step its scratch folder:\n%s", brief)
	}

	change, err := readChange(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range change.Files {
		paths = append(paths, f.Path)
	}
	if len(paths) != 1 || paths[0] != "ledger.go" {
		t.Fatalf("the change is %v, want ledger.go alone", paths)
	}

	if err := commitApproved(ExecGit{}, dir, job.Item, ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(mustGit(t, dir, "diff", "--name-only", base, "HEAD")); len(got) != 1 || got[0] != "ledger.go" {
		t.Fatalf("the commit holds %v, want ledger.go alone", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".proof-prefix-15992", "ledger.go")); err != nil {
		t.Fatal("the copy was deleted; it stays on disk for the person to look at")
	}
}

// A copy a step committed itself is taken out of the published tree too, and
// a new package that is not a copy of the repository is the change.
func TestACommittedCopyIsTakenOutAndANewPackageStays(t *testing.T) {
	tracked := map[string]bool{"ledger.go": true, "auth/token.go": true, "README.md": true, "cmd/main.go": true}
	if got := repoCopies([]string{".copy/ledger.go", ".copy/auth/token.go", "count/count.go", "count/count_test.go", "v2/ledger.go"}, tracked); len(got) != 1 || got[0] != ".copy" {
		t.Fatalf("copies = %v, want .copy alone", got)
	}
	if got := repoCopies([]string{"snapshot/ledger.go", "snapshot/auth/token.go", "snapshot/README.md"}, tracked); len(got) != 1 {
		t.Fatalf("a visible copy of most of the repository was kept: %v", got)
	}

	dir := gitRig(t)
	mustGit(t, dir, "checkout", "-q", "-b", "factory/148")
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("# api\n"), 0o600)
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "readme")
	base := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD"))
	for _, name := range []string{"ledger.go", "README.md"} {
		src, _ := os.ReadFile(filepath.Join(dir, name))
		_ = os.MkdirAll(filepath.Join(dir, ".proof-prefix-1"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, ".proof-prefix-1", name), src, 0o600)
	}
	_ = os.WriteFile(filepath.Join(dir, "count.go"), []byte("package ledger\n"), 0o600)
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "a step committed its scratch")
	_ = os.WriteFile(filepath.Join(dir, "count_test.go"), []byte("package ledger\n"), 0o600)
	if err := commitApproved(ExecGit{}, dir, factory.Item{Title: "count", ChangeBase: base}, ""); err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(mustGit(t, dir, "diff", "--name-only", base, "HEAD"))
	if strings.Join(got, " ") != "count.go count_test.go" {
		t.Fatalf("the published tree holds %v, want count.go count_test.go", got)
	}
}
