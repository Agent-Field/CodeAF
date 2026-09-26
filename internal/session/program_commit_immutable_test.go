package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A signed commit is durable evidence. Finishing a clean run preserves its
// hash, signature, and complete object bytes.
func TestProgramFinishLeavesSignedTipUntouched(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is needed to sign the fixture commit")
	}
	repo := newTestRepo(t)
	key := filepath.Join(t.TempDir(), "signing-key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "Signed work", Holder: "task 9", Keep: t.TempDir(), SignModel: "fixture/vendor-model"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(folder.release)
	if err := os.WriteFile(filepath.Join(repo, "signed.txt"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "signed.txt")
	mustGit(t, repo, "config", "gpg.format", "ssh")
	mustGit(t, repo, "config", "user.signingkey", key)
	mustGit(t, repo, "-c", "user.name="+codeafGitName, "-c", "user.email="+codeafGitEmail, "commit", "-q", "-S", "-m", "model signed work")
	beforeHash := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	before := gitOut(t, repo, "cat-file", "-p", "HEAD")
	if !strings.Contains(before, "gpgsig") {
		t.Fatal("fixture did not sign")
	}
	folder.Finish("done")
	after := gitOut(t, repo, "cat-file", "-p", "HEAD")
	if afterHash := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); afterHash != beforeHash {
		t.Fatalf("finish rewrote signed commit: %s -> %s", beforeHash, afterHash)
	}
	if after != before {
		t.Fatal("finish changed the signed commit object")
	}
}

// A push to a direct URL creates no remote-tracking ref, but the pushed commit
// is still immutable at finish.
func TestProgramFinishLeavesDirectURLPushUntouched(t *testing.T) {
	repo := newTestRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("bare remote: %v: %s", err, out)
	}
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: testPrograms("fake")[0], Dir: repo, Title: "Pushed work", Holder: "task 9", Keep: t.TempDir(), SignModel: "fixture/vendor-model"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(folder.release)
	if err := os.WriteFile(filepath.Join(repo, "pushed.txt"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "pushed.txt")
	mustGit(t, repo, "-c", "user.name="+codeafGitName, "-c", "user.email="+codeafGitEmail, "commit", "-q", "-m", "model work")
	mustGit(t, repo, "push", "-q", remote, "HEAD:refs/heads/task")
	before := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	object := gitOut(t, repo, "cat-file", "-p", "HEAD")
	folder.Finish("done")
	after := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))
	if after != before {
		t.Fatalf("finish rewrote pushed commit via direct URL: pushed=%s local=%s", before, after)
	}
	if afterObject := gitOut(t, repo, "cat-file", "-p", "HEAD"); afterObject != object {
		t.Fatal("finish changed the pushed commit object")
	}
}
