//go:build linux || darwin

package taskdoor_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

func copyHomes(t *testing.T) (taskdoor.CopyDeps, taskdoor.CopyDeps) {
	t.Helper()
	r := newRig(t)
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	mem := blobstore.NewMemory()
	srv := httptest.NewServer(blobstore.Handler(func(*http.Request, []byte) (string, string, error) {
		return r.id.ID(), r.a.ID(), nil
	}, func(string) (blobstore.Store, error) { return mem, nil }))
	t.Cleanup(srv.Close)
	makeHome := func(devHome string) taskdoor.CopyDeps {
		engine := cellstore.EngineFor("")
		engine.DataRoot = filepath.Join(devHome, "stores")
		engine.Binary = bin
		// The fixture owns its daemon so no engine can outlive the test's store.
		socketHome, err := os.MkdirTemp("", "rn-l1-")
		if err != nil {
			t.Fatal(err)
		}
		daemon := cellstore.Daemon{Binary: bin, Socket: filepath.Join(socketHome, "e.sock")}
		engine.Transport = daemon
		t.Cleanup(func() { _ = daemon.Stop(context.Background()); _ = os.RemoveAll(socketHome) })
		return taskdoor.CopyDeps{Root: filepath.Join(devHome, "copies"), Service: srv.URL,
			Identity: r.id, Device: r.a, Engine: engine, Store: blobstore.NewHTTP(srv.URL, func(*http.Request, []byte) {}, srv.Client())}
	}
	a, b := makeHome(t.TempDir()), makeHome(t.TempDir())
	b.Device = r.b
	return a, b
}

func copyGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=task test", "-c", "user.email=task@example.test"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func copyFile(t *testing.T, dir, name, words string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(words), 0640); err != nil {
		t.Fatal(err)
	}
}

func copyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	copyGit(t, dir, "init", "-q", "-b", "task-copy")
	copyFile(t, dir, "edit.txt", "before\n")
	copyFile(t, dir, "delete.txt", "remove this\n")
	copyFile(t, dir, ".gitignore", "ignored/\n")
	copyGit(t, dir, "add", "--all")
	copyGit(t, dir, "commit", "-qm", "baseline")
	return dir
}

// Contract 3: the real carrier, engine and blob wire preserve files and commits
// across two homes while the chat and its directory never enter the transfer.
func TestATaskCopyGoesOutAndComesBackWithItsEdits(t *testing.T) {
	a, b := copyHomes(t)
	local, work := copyRepo(t), filepath.Join(t.TempDir(), "work")
	copyFile(t, local, "ignored/cache", "only here")
	copyFile(t, local, "uncommitted.txt", "carried too")
	if err := os.Symlink("edit.txt", filepath.Join(local, "link")); err != nil {
		t.Fatal(err)
	}
	in, err := taskdoor.SendCopy(context.Background(), a, local)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskdoor.ReceiveCopy(context.Background(), b, in, work); err != nil {
		t.Fatal(err)
	}
	if got := copyGit(t, work, "rev-parse", "HEAD"); got != copyGit(t, local, "rev-parse", "HEAD") {
		t.Fatal("HEAD did not travel")
	}
	if got := copyGit(t, work, "branch", "--show-current"); got != "task-copy" {
		t.Fatal(got)
	}
	if info, err := os.Stat(filepath.Join(work, "uncommitted.txt")); err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("mode: %v, %v", info, err)
	}
	if link, err := os.Readlink(filepath.Join(work, "link")); err != nil || link != "edit.txt" {
		t.Fatalf("link: %s, %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(work, "ignored")); !os.IsNotExist(err) {
		t.Fatalf("ignored files traveled: %v", err)
	}
	copyFile(t, work, "edit.txt", "from B\n")
	copyFile(t, work, "added.txt", "new on B\n")
	if err := os.Remove(filepath.Join(work, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	copyGit(t, work, "add", "--all")
	copyGit(t, work, "commit", "-qm", "finished on B")
	result, err := taskdoor.SendResult(context.Background(), b, work, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskdoor.ReceiveResult(context.Background(), a, result, local, in.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if got := copyGit(t, local, "rev-parse", "HEAD"); got != copyGit(t, work, "rev-parse", "HEAD") {
		t.Fatal("B's commit did not come home")
	}
	for _, dir := range []string{local, work} {
		for _, name := range []string{".cell", ".furrow", ".furrowpolicy", "delete.txt"} {
			if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Errorf("%s left %s: %v", dir, name, err)
			}
		}
		for name, want := range map[string]string{"edit.txt": "from B\n", "added.txt": "new on B\n", "uncommitted.txt": "carried too"} {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || string(got) != want {
				t.Errorf("%s/%s = %q, %v", dir, name, got, err)
			}
		}
	}
}

// Contract 3: work done here while B runs cannot be overwritten by its result.
func TestAResultIsRefusedWhenTheCopyChangedHere(t *testing.T) {
	a, b := copyHomes(t)
	local, work := copyRepo(t), filepath.Join(t.TempDir(), "work")
	in, err := taskdoor.SendCopy(context.Background(), a, local)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskdoor.ReceiveCopy(context.Background(), b, in, work); err != nil {
		t.Fatal(err)
	}
	copyFile(t, work, "edit.txt", "B's work")
	result, err := taskdoor.SendResult(context.Background(), b, work, in)
	if err != nil {
		t.Fatal(err)
	}
	copyFile(t, local, "edit.txt", "changed here")
	before := copyGit(t, local, "status", "--porcelain")
	if err := taskdoor.ReceiveResult(context.Background(), a, result, local, in.Fingerprint); err == nil {
		t.Fatal("changed copy accepted")
	}
	got, err := os.ReadFile(filepath.Join(local, "edit.txt"))
	if err != nil || string(got) != "changed here" {
		t.Fatalf("local work changed: %q, %v", got, err)
	}
	if after := copyGit(t, local, "status", "--porcelain"); after != before {
		t.Fatalf("refusal changed copy: %s", after)
	}
}

// Contract 3 includes linked worktrees: normal landing reads the ground's
// shared task branch, so receiving a result must retain that registration.
func TestALinkedTaskCopyKeepsItsBranchForNormalLanding(t *testing.T) {
	a, b := copyHomes(t)
	ground := copyRepo(t)
	local := filepath.Join(t.TempDir(), "task")
	copyGit(t, ground, "worktree", "add", "-q", "-b", "task-linked", local, "HEAD")
	in, err := taskdoor.SendCopy(t.Context(), a, local)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "work")
	if err := taskdoor.ReceiveCopy(t.Context(), b, in, work); err != nil {
		t.Fatal(err)
	}
	copyFile(t, work, "edit.txt", "the returned commit\n")
	copyGit(t, work, "add", "--all")
	copyGit(t, work, "commit", "-qm", "the remote task")
	result, err := taskdoor.SendResult(t.Context(), b, work, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskdoor.ReceiveResult(t.Context(), a, result, local, in.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(local, ".git")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("lost linked registration: %v, %v", info, err)
	}
	want := copyGit(t, work, "rev-parse", "HEAD")
	if got := copyGit(t, ground, "rev-parse", "task-linked"); got != want {
		t.Fatalf("landing branch=%s want %s", got, want)
	}
	if got := copyGit(t, local, "rev-parse", "HEAD"); got != want {
		t.Fatalf("task copy=%s want %s", got, want)
	}
	if status := copyGit(t, local, "status", "--porcelain"); status != "" {
		t.Fatalf("returned commit has spurious edits: %s", status)
	}
}

// Contract 3's refusal check follows the potentially slow history import, so
// an editor active during that import cannot lose its work to the result.
func TestAResultKeepsAnEditMadeWhileItsHistoryArrives(t *testing.T) {
	a, b := copyHomes(t)
	ground := copyRepo(t)
	local := filepath.Join(t.TempDir(), "task")
	copyGit(t, ground, "worktree", "add", "-q", "-b", "task-linked", local, "HEAD")
	oldHead := copyGit(t, local, "rev-parse", "HEAD")
	in, err := taskdoor.SendCopy(t.Context(), a, local)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "work")
	if err := taskdoor.ReceiveCopy(t.Context(), b, in, work); err != nil {
		t.Fatal(err)
	}
	copyFile(t, work, "edit.txt", "B's change")
	copyGit(t, work, "add", "--all")
	copyGit(t, work, "commit", "-qm", "remote work")
	result, err := taskdoor.SendResult(t.Context(), b, work, in)
	if err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := t.TempDir()
	script := "#!/bin/sh\nfor arg in \"$@\"; do\n if [ \"$arg\" = fetch ]; then printf 'edited during import' > \"$CODEAF_TEST_EDIT_DURING_FETCH\"; fi\ndone\nexec \"$CODEAF_TEST_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrapper, "git"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_TEST_REAL_GIT", realGit)
	t.Setenv("CODEAF_TEST_EDIT_DURING_FETCH", filepath.Join(local, "edit.txt"))
	t.Setenv("PATH", wrapper+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := taskdoor.ReceiveResult(t.Context(), a, result, local, in.Fingerprint); !errors.Is(err, taskdoor.ErrCopyChanged) {
		t.Fatalf("concurrent edit accepted: %v", err)
	}
	if got := copyGit(t, local, "rev-parse", "HEAD"); got != oldHead {
		t.Fatal("refusal moved task branch")
	}
	if raw, err := os.ReadFile(filepath.Join(local, "edit.txt")); err != nil || string(raw) != "edited during import" {
		t.Fatalf("local edit lost: %q, %v", raw, err)
	}
}

// Contract 15: a second send pays for the edited file and new structure only.
func TestASecondSendUploadsOnlyTheChange(t *testing.T) {
	if testing.Short() {
		t.Skip("10,000-file measurement")
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	a, b := copyHomes(t)
	local := copyRepo(t)
	for n := 0; n < 10000; n++ {
		copyFile(t, local, fmt.Sprintf("files/%05d.txt", n), fmt.Sprintf("project file %d\n", n))
	}
	copyGit(t, local, "add", "--all")
	copyGit(t, local, "commit", "-qm", "project files")
	for n := 0; n < 2; n++ {
		if n == 1 {
			copyFile(t, local, "files/00001.txt", "one changed file\n")
		}
		var seal, upload, fetch, restore taskdoor.CopyMeasure
		a.Measure = func(m taskdoor.CopyMeasure) { seal, upload = m, m }
		b.Measure = func(m taskdoor.CopyMeasure) { fetch, restore = m, m }
		in, err := taskdoor.SendCopy(ctx, a, local)
		if err != nil {
			t.Fatal(err)
		}
		if err := taskdoor.ReceiveCopy(ctx, b, in, filepath.Join(t.TempDir(), "work")); err != nil {
			t.Fatal(err)
		}
		t.Logf("delta-measure: send=%d seal=%s upload=%s fetch=%s restore=%s bytes=%d", n+1, seal.Seal, upload.Upload, fetch.Fetch, restore.Restore, upload.Bytes)
		if n == 1 && upload.Bytes >= 256<<10 {
			t.Fatalf("second upload = %d bytes", upload.Bytes)
		}
	}
	if elapsed := time.Since(started); elapsed >= time.Minute {
		t.Fatalf("delta measurement exceeded one minute: %s", elapsed)
	}
}
