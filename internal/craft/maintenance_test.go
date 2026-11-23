package craft

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The craft history has no other git to clean it up, so automatic maintenance
// stays on there, but it must finish inside the save: a detached gc would keep
// writing after codeaf has moved on.
func TestCraftSaveFinishesAutomaticMaintenanceBeforeReturning(t *testing.T) {
	repo := openRepo(t)
	gitIn := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repo.dir
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitIn("config", "maintenance.strategy", "gc")
	gitIn("config", "gc.auto", "1")
	// gc.autoDetach is left at git's default (on): the save must turn it off.
	loose := 2000
	if n, err := strconv.Atoi(os.Getenv("CODEAF_CRAFT_GC_LOOSE")); err == nil && n > 0 {
		loose = n
	}
	blobs := t.TempDir()
	for i := 0; i < loose; i++ {
		p := filepath.Join(blobs, strconv.Itoa(i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("loose object %d\n", i)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Reachable objects, so a real gc packs them; unreachable recent objects
	// would stay loose and prove nothing.
	index := filepath.Join(t.TempDir(), "index")
	add := exec.Command("git", "--work-tree="+blobs, "add", ".")
	add.Dir = repo.dir
	add.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	write := exec.Command("git", "write-tree")
	write.Dir = repo.dir
	write.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
	tree, err := write.Output()
	if err != nil {
		t.Fatalf("write-tree: %v", err)
	}
	commit := gitIn("-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit-tree", strings.TrimSpace(string(tree)), "-m", "bulk")
	gitIn("update-ref", "refs/heads/bulk", commit)
	hooks := gitIn("rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(repo.dir, hooks)
	}
	marker := filepath.Join(t.TempDir(), "pre-auto-gc-ran")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-auto-gc"), []byte("#!/bin/sh\necho ran > '"+marker+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	w := parseValid(t, presentationYAML)
	started := time.Now()
	if _, err := repo.Save(w, "a save that crosses the gc threshold"); err != nil {
		t.Fatalf("save: %v", err)
	}
	took := time.Since(started)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("automatic maintenance did not run during the save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.dir, ".git", "gc.pid")); err == nil {
		t.Fatal("a gc is still running after the save returned")
	}
	counts := gitIn("count-objects", "-v")
	for _, line := range strings.Split(counts, "\n") {
		if strings.HasPrefix(line, "count: ") && strings.TrimPrefix(line, "count: ") != "0" {
			t.Fatalf("loose objects remain right after the save, so gc was detached: %s", line)
		}
	}
	t.Logf("CRAFT_GC_SAVE loose=%d took=%s", loose, took)
}
