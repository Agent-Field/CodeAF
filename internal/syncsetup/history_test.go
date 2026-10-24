package syncsetup

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/home"
)

// TestC9CommittedHistoryTravelsSealedAndWorkingSecretsUseTheVault proves the
// history half of C9 as decided by Abir on 2026-10-21. A deleted historical key
// must remain readable on the second home, with all commits and branches intact,
// while an untracked .env arrives only through the vault. Every request body at
// the in-process relay must be free of both plaintext keys on both transports.
func TestC9CommittedHistoryTravelsSealedAndWorkingSecretsUseTheVault(t *testing.T) {
	for _, transport := range []string{"spawn", "daemon"} {
		t.Run(transport, func(t *testing.T) {
			const historyKey = "sk-or-v1-0123456789abcdef0123456789abcdef"
			const workingKey = "sk-or-v1-fedcba9876543210fedcba9876543210"
			var leaked, uploaded atomic.Bool
			observe := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					raw, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					if bytes.Contains(raw, []byte(historyKey)) || bytes.Contains(raw, []byte(workingKey)) {
						leaked.Store(true)
					}
					if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/frames") {
						uploaded.Store(true)
					}
					r.Body = io.NopCloser(bytes.NewReader(raw))
					next.ServeHTTP(w, r)
				})
			}
			h := newTwoHomes(t, observe)
			t.Setenv(home.EnvVar, h.homeA)
			h.engine.Guard.Home = h.homeA
			if transport == "daemon" {
				dir, err := os.MkdirTemp("", "history-daemon-")
				if err != nil {
					t.Fatal(err)
				}
				d := cellstore.Daemon{Binary: h.bin, Socket: filepath.Join(dir, "e.sock")}
				t.Cleanup(func() { _ = d.Stop(context.Background()); _ = os.RemoveAll(dir) })
				h.engine.Transport, h.engB.Transport = d, d
			}
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(h.work, path), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			git(t, h.work, "init", "-q", "-b", "trunk")
			const historicalFile = "retired-key.txt"
			historicalBytes := "API_KEY=" + historyKey + "\n"
			write(historicalFile, historicalBytes)
			write("main.py", "print('safe')\n")
			git(t, h.work, "add", historicalFile, "main.py")
			git(t, h.work, "commit", "-qm", "past key")
			blob := gitOut(t, h.work, "rev-parse", "HEAD:"+historicalFile)
			git(t, h.work, "branch", "archive/with-key")
			if err := os.Remove(filepath.Join(h.work, historicalFile)); err != nil {
				t.Fatal(err)
			}
			git(t, h.work, "add", "-u")
			git(t, h.work, "commit", "-qm", "delete key from tip")
			// A task's own commit must travel through its bundle as well as .git.
			task := filepath.Join(h.cell.Root, cell.TreesDir, "history")
			git(t, h.work, "worktree", "add", "-q", "-b", "task/history", task)
			if err := os.WriteFile(filepath.Join(task, "task-key.txt"), []byte(historicalBytes), 0o600); err != nil {
				t.Fatal(err)
			}
			git(t, task, "add", "task-key.txt")
			git(t, task, "commit", "-qm", "task key")
			taskHead := gitOut(t, task, "rev-parse", "HEAD")
			wantLog := gitOut(t, h.work, "log", "--all", "--format=%H %s")
			wantBranches := gitOut(t, h.work, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
			env := "LOCAL_KEY=" + workingKey + "\n"
			write(".env", env)
			if out := gitOut(t, h.work, "status", "--porcelain"); out != "?? .env" {
				t.Fatalf("working secret must be untracked: %q", out)
			}
			a := h.openA()
			a.mustSay("saved the repository")
			a.mustSay("saved another turn")
			h.durable(h.cell.ID, h.cell)
			if err := a.drive.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			// Fetching only the chat's sealed objects cannot obtain the .env.
			probe, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatal(err)
			}
			work := t.TempDir()
			eng := cellstore.Engine{Binary: h.bin, DataRoot: t.TempDir(), Workspace: work, Transport: h.engine.Transport}
			sync := eng.Sync(cellstore.SyncKeys{CellKey: h.b.Identity.CellKey(), Dedup: h.b.Identity.DedupSecret()}, h.b.Ledger)
			fetch := cellsync.Fetcher{Engine: sync, Store: h.b.Store, Inbox: sync.Inbox}
			if err := fetch.Fetch(context.Background(), probe, h.head()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(filepath.Join(work, ".env")); !os.IsNotExist(err) {
				t.Fatalf("chat objects alone restored the working secret: %v", err)
			}
			if got := gitOut(t, work, "cat-file", "-p", blob); got != strings.TrimSpace(historicalBytes) {
				t.Fatalf("sealed history lost its key-bearing object: %q", got)
			}
			bundle := filepath.Join(probe.Root, cell.StateDir, cell.TreesDir, "history", "commits.bundle")
			if _, err := os.Stat(bundle); err != nil {
				t.Fatalf("task commit bundle did not travel: %v", err)
			}
			t.Setenv(home.EnvVar, h.homeB)
			h.engB.Guard.Home = h.homeB
			// B cannot see A's task folders. As in the state audit, remove the
			// fixture's source copy so Git cannot mistake it for a local worktree.
			if err := os.RemoveAll(filepath.Join(h.cell.Root, cell.TreesDir)); err != nil {
				t.Fatal(err)
			}
			got, err := h.continuerB().Take(context.Background(), h.cell.ID)
			if err != nil {
				t.Fatal(err)
			}
			restored := workspaceOf(got.Taken.Cell.Root)
			if got := gitOut(t, restored, "log", "--all", "--format=%H %s"); got != wantLog {
				t.Fatalf("history changed on arrival:\n%s\nwant:\n%s", got, wantLog)
			}
			if got := gitOut(t, restored, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/"); got != wantBranches {
				t.Fatalf("branches changed on arrival:\n%s\nwant:\n%s", got, wantBranches)
			}
			if got := gitOut(t, restored, "cat-file", "-p", blob); got != strings.TrimSpace(historicalBytes) {
				t.Fatalf("historical key-bearing blob changed: %q", got)
			}
			if _, err := os.Stat(filepath.Join(restored, historicalFile)); !os.IsNotExist(err) {
				t.Fatalf("the deleted historical file reappeared at the tip: %v", err)
			}
			if got := gitOut(t, filepath.Join(got.Taken.Cell.Root, cell.TreesDir, "history"), "rev-parse", "HEAD"); got != taskHead {
				t.Fatalf("task commit changed on arrival: %q, want %q", got, taskHead)
			}
			if raw, err := os.ReadFile(filepath.Join(restored, ".env")); err != nil || string(raw) != env {
				t.Fatalf("vault restored .env as %q, %v", raw, err)
			}
			if !uploaded.Load() || leaked.Load() {
				t.Fatalf("relay upload observed %v, plaintext key received %v", uploaded.Load(), leaked.Load())
			}
		})
	}
}
