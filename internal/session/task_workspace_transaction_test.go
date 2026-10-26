package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/home"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

func TestTaskWorkspaceTransactionRejectsChangedPermissions(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, false)
	path := filepath.Join(source, "shared.txt")
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
		t.Fatal("a metadata-only change in the original was overwritten")
	}
	if got := readFile(t, path); got != "the original line\n" {
		t.Fatalf("original contents changed: %q", got)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("original permission change lost: %v %v", info, err)
	}
	if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private result\n" {
		t.Fatalf("private copy changed: %q", got)
	}
}

func TestTaskWorkspaceTransactionGitRefFailureRollsBackPortableState(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixtureWithInput(t, true, func(root string) {
		if err := os.Symlink("shared.txt", filepath.Join(root, "pointer")); err != nil {
			t.Fatal(err)
		}
		mustGit(t, root, "add", "--", "pointer")
		mustGit(t, root, append(codeafGitIdentity(), "commit", "-m", "include symlink input")...)
	})
	if err := os.Remove(filepath.Join(copyDir, "pointer")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(copyDir, "pointer")); err != nil {
		t.Fatal(err)
	}
	mustGit(t, source, "pack-refs", "--all")
	writeFile(t, filepath.Join(copyDir, "added.txt"), "added raw\r\n")
	writeFile(t, filepath.Join(copyDir, "shared.txt"), "reviewed raw\r\nbytes\r\n")
	if err := os.Chmod(filepath.Join(copyDir, "shared.txt"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(copyDir, "empty"), 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	before, err := taskcopy.InspectPortableState(source)
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(gitOut(t, source, "rev-parse", "HEAD"))
	indexPath := strings.TrimSpace(gitOut(t, source, "rev-parse", "--path-format=absolute", "--git-path", "index"))
	beforeIndex, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	branch := currentBranch(source)
	lockPath := strings.TrimSpace(gitOut(t, source, "rev-parse", "--path-format=absolute", "--git-path", "refs/heads/"+branch+".lock"))
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("another Git writer owns this lock\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(lockPath) })
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil {
		t.Fatal("apply succeeded despite unavailable branch publication")
	}
	if err := taskcopy.VerifyPortableState(source, before); err != nil {
		t.Fatalf("Git publication failure left a partial portable apply: %v", err)
	}
	if got := strings.TrimSpace(gitOut(t, source, "rev-parse", "HEAD")); got != head {
		t.Fatal("Git publication failure moved HEAD")
	}
	afterIndex, err := os.ReadFile(indexPath)
	if err != nil || string(afterIndex) != string(beforeIndex) {
		t.Fatalf("Git publication failure changed index: %v", err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatalf("retry after verified rollback: %v", err)
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "reviewed raw\r\nbytes\r\n" {
		t.Fatalf("raw bytes changed: %q", got)
	}
	if link, err := os.Readlink(filepath.Join(source, "pointer")); err != nil || link != "missing-target" {
		t.Fatalf("reviewed symlink changed: %q %v", link, err)
	}
	for path, mode := range map[string]os.FileMode{"shared.txt": 0640, "empty": 0750} {
		info, err := os.Stat(filepath.Join(source, path))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("portable metadata %s: %v %v", path, info, err)
		}
	}
}

func TestTaskWorkspaceTransactionPreservesUnrelatedStagedAndDirtyFiles(t *testing.T) {
	agent, id, _, source, _ := completedWorkspaceApplyFixture(t, true)
	writeFile(t, filepath.Join(source, "staged.txt"), "the person's staged content\n")
	mustGit(t, source, "add", "--", "staged.txt")
	writeFile(t, filepath.Join(source, "dirty.txt"), "the person's unrelated content\n")
	staged := gitOut(t, source, "diff", "--cached", "--binary")
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if after := gitOut(t, source, "diff", "--cached", "--binary"); after != staged {
		t.Fatalf("unrelated staged changes were consumed or changed:\nbefore:%s\nafter:%s", staged, after)
	}
	if got := readFile(t, filepath.Join(source, "dirty.txt")); got != "the person's unrelated content\n" {
		t.Fatalf("unrelated dirty file changed: %q", got)
	}
}

func TestTaskWorkspaceTransactionPreservesRawBytesWithCleanGitRepresentation(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, true)
	mustGit(t, source, "config", "core.safecrlf", "warn")
	writeFile(t, filepath.Join(copyDir, ".gitattributes"), "*.txt text eol=lf\n")
	writeFile(t, filepath.Join(copyDir, "shared.txt"), "reviewed CRLF\r\n")
	if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "reviewed CRLF\r\n" {
		t.Fatalf("working bytes normalized: %q", got)
	}
	if status := strings.TrimSpace(gitOut(t, source, "status", "--porcelain", "--untracked-files=all")); status != "" {
		t.Fatalf("applied Git representation is dirty: %s", status)
	}
	if blob := gitOut(t, source, "show", "HEAD:shared.txt"); blob != "reviewed CRLF\n" {
		t.Fatalf("Git clean representation wrong: %q", blob)
	}
}

func TestTaskWorkspaceTransactionCheckpointFailureReplaysWithoutApplyingAgain(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fault requires an unprivileged process")
	}
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-process", true: "reopened-agent"}[reopen], func(t *testing.T) {
			agent, id, place, source, _ := completedWorkspaceApplyFixture(t, true)
			if err := os.Chmod(place.Dir, 0500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(place.Dir, 0700) })
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "could not be saved") {
				t.Fatalf("fault did not reach checkpoint after application: %v", err)
			}
			if got := readFile(t, filepath.Join(source, "shared.txt")); got != "private result\n" {
				t.Fatalf("checkpoint fault did not occur after application: %q", got)
			}
			head := gitOut(t, source, "rev-parse", "HEAD")
			if reopen {
				_ = agent.Close()
			}
			if err := os.Chmod(place.Dir, 0700); err != nil {
				t.Fatal(err)
			}
			if reopen {
				agent, _ = newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace, c.Place, c.SessionFile = source, place, place.Transcript() })
			}
			if reopen {
				paths, err := filepath.Glob(home.Join("workspace-apply", "*.json"))
				if err != nil || len(paths) != 1 {
					t.Fatalf("review media unavailable: %v %v", paths, err)
				}
				raw, err := os.ReadFile(paths[0])
				if err != nil {
					t.Fatal(err)
				}
				var media struct{ Place string }
				if err := json.Unmarshal(raw, &media); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(media.Place, "portable", "shared.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "/task apply") {
				t.Fatalf("pending checkpoint review replaced the recoverable ticket: %v", err)
			}
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatalf("committed checkpoint replay failed: %v", err)
			}
			if after := gitOut(t, source, "rev-parse", "HEAD"); after != head {
				t.Fatal("checkpoint retry created a second Git application")
			}
		})
	}
}

func TestTaskWorkspaceTransactionPreservesGitFilenames(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, true)
	paths := []string{"has space.txt", "line\nbreak.txt", "-leading.txt", "café-文件.txt"}
	for _, path := range paths {
		writeFile(t, filepath.Join(copyDir, path), "reviewed "+path+"\r\n")
	}
	if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if got := readFile(t, filepath.Join(source, path)); got != "reviewed "+path+"\r\n" {
			t.Fatalf("wrong bytes for %q: %q", path, got)
		}
	}
	if status := strings.TrimSpace(gitOut(t, source, "status", "--porcelain", "--untracked-files=all")); status != "" {
		t.Fatalf("applied filename set left dirty: %s", status)
	}
}

func TestTaskWorkspaceTransactionApplyScale(t *testing.T) {
	for _, count := range []int{100, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, true)
			for index := 0; index < count; index++ {
				writeFile(t, filepath.Join(copyDir, fmt.Sprintf("result-%04d.txt", index)), "reviewed result\r\n")
			}
			if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			t.Logf("public Apply %d added files: %s", count, time.Since(started))
			for _, index := range []int{0, count - 1} {
				if got := readFile(t, filepath.Join(source, fmt.Sprintf("result-%04d.txt", index))); got != "reviewed result\r\n" {
					t.Fatal("scale apply lost bytes")
				}
			}
		})
	}
}

func TestTaskWorkspaceTransactionRefusesHookModifiedOutput(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, true)
	hooks := t.TempDir()
	mustGit(t, source, "config", "core.hooksPath", hooks)
	script := "#!/bin/sh\nif [ \"$1\" = committed ] && [ ! -e hook-ran ]; then\n printf happened > hook-ran\n printf 'external hook edit\\n' > shared.txt\nfi\n"
	if err := os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "recovery retained") {
		t.Fatalf("hook changed output but apply did not refuse exact success with retained recovery: %v", err)
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "external hook edit\n" {
		t.Fatalf("hook edit overwritten: %q", got)
	}
	if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private result\n" {
		t.Fatal("private output changed")
	}
}

func TestTaskWorkspaceTransactionCancellationBeforeCommitDecisionRollsBack(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, true)
	head := gitOut(t, source, "rev-parse", "HEAD")
	hooks := t.TempDir()
	mustGit(t, source, "config", "core.hooksPath", hooks)
	script := "#!/bin/sh\nif [ \"$1\" = committed ] && [ ! -e hook-ran ]; then\n printf happened > hook-ran\n n=0\n while [ ! -e hook-released ] && [ $n -lt 200 ]; do n=$((n+1)); sleep 0.01; done\nfi\n"
	if err := os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := agent.ApplyTaskWorkspaceRoot(ctx, id, "work"); finished <- err }()
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("publication hook was never reached")
		case err := <-finished:
			t.Fatalf("apply returned before cancellation boundary: %v", err)
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(source, "hook-ran")); err == nil {
				cancel()
				writeFile(t, filepath.Join(source, "hook-released"), "release\n")
				if err := <-finished; !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel before COMMITTED did not roll back: %v", err)
				}
				if got := readFile(t, filepath.Join(source, "shared.txt")); got != "the original line\n" {
					t.Fatalf("cancel left applied bytes: %q", got)
				}
				if got := gitOut(t, source, "rev-parse", "HEAD"); got != head {
					t.Fatal("cancel left applied branch")
				}
				if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private result\n" {
					t.Fatal("private output changed")
				}
				return
			}
		}
	}
}

func TestTaskWorkspaceTransactionRefusesSplitIndexBeforeMutation(t *testing.T) {
	agent, id, _, source, copyDir := completedWorkspaceApplyFixture(t, true)
	mustGit(t, source, "update-index", "--split-index")
	head := gitOut(t, source, "rev-parse", "HEAD")
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "split index") {
		t.Fatalf("split index did not refuse before mutation: %v", err)
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "the original line\n" {
		t.Fatalf("unsupported Git storage changed bytes: %q", got)
	}
	if got := gitOut(t, source, "rev-parse", "HEAD"); got != head {
		t.Fatal("unsupported Git storage moved HEAD")
	}
	if got := readFile(t, filepath.Join(copyDir, "shared.txt")); got != "private result\n" {
		t.Fatal("unsupported Git storage changed private copy")
	}
}

func TestTaskWorkspaceTransactionRefusesConfiguredSplitPreference(t *testing.T) {
	agent, id, _, source, _ := completedWorkspaceApplyFixture(t, true)
	mustGit(t, source, "config", "core.splitIndex", "true")
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err == nil || !strings.Contains(err.Error(), "split index") {
		t.Fatalf("configured split index did not refuse: %v", err)
	}
	if got := readFile(t, filepath.Join(source, "shared.txt")); got != "the original line\n" {
		t.Fatalf("unsupported configuration changed output: %q", got)
	}
	if value := strings.TrimSpace(gitOut(t, source, "config", "--get", "core.splitIndex")); value != "true" {
		t.Fatal("person's Git configuration changed")
	}
}

func TestTaskWorkspaceTransactionKeepsRootGroupAndNormalizesNewDirectory(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	secondary := -1
	for _, group := range groups {
		if group != os.Getgid() {
			secondary = group
			break
		}
	}
	if secondary < 0 {
		t.Skip("requires a second supplementary group for inheritance fixture")
	}
	agent, id, _, source, copyDir := completedWorkspaceApplyFixtureWithInput(t, false, func(root string) {
		if err := os.Chown(root, -1, secondary); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Mkdir(filepath.Join(copyDir, "new-directory"), 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
		t.Fatal(err)
	}
	if err := taskcopy.CheckPortableMutationMetadata(source, []string{"new-directory"}); err != nil {
		t.Fatalf("new directory group is not supported primary group: %v", err)
	}
	if err := taskcopy.CheckPortableMutationMetadata(source, []string{"."}); err == nil || !strings.Contains(err.Error(), "different owner or group") {
		t.Fatalf("existing project root group changed: %v", err)
	}
}
