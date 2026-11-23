package syncsetup

import (
	"crypto/sha256"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQALargeRepositoryMovesWithHistoryAndExactBytes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	h := newTwoHomes(t)
	git := func(dir string, args ...string) string {
		t.Helper()
		argv := append([]string{"-C", dir, "-c", "user.name=fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false"}, args...)
		cmd := exec.CommandContext(t.Context(), "git", argv...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v, %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(h.work, "init", "-q")
	rng := rand.NewChaCha8([32]byte{74, 11, 53})
	file := filepath.Join("models", "fixture.bin")
	mustNil(t, os.MkdirAll(filepath.Join(h.work, "models"), 0700))
	// Distinct binary bytes spanning multiple transport frames. The second
	// commit changes only a small text file, leaving a sizable git history.
	blob := make([]byte, 20<<20)
	_, err := rng.Read(blob)
	mustNil(t, err)
	sum := sha256.Sum256(blob)
	mustNil(t, os.WriteFile(filepath.Join(h.work, file), blob, 0600))
	git(h.work, "add", ".")
	git(h.work, "commit", "-qm", "add fixture")
	mustNil(t, os.WriteFile(filepath.Join(h.work, "README.md"), []byte("expected worktree\n"), 0600))
	git(h.work, "add", ".")
	git(h.work, "commit", "-qm", "add readme")
	head := git(h.work, "rev-parse", "HEAD")

	a := h.openA()
	a.mustSay("save this repository")
	moved := h.takeOnB(a)
	mustNil(t, moved.Wait(t.Context()))
	here := workspaceOf(moved.Taken.Cell.Root)
	if here == h.work || git(here, "rev-parse", "HEAD") != head || git(here, "rev-list", "--count", "HEAD") != "2" {
		t.Fatal("the receiver did not get the repository's committed history")
	}
	received, err := os.ReadFile(filepath.Join(here, file))
	mustNil(t, err)
	if sha256.Sum256(received) != sum {
		t.Fatal("the received binary differs from the saved one")
	}
}
