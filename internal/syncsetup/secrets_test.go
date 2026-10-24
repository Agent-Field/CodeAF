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

// TestC9RelayReceivesNoPlaintextSecretsAndTakeUsesTheVault proves C9's
// working-tree law at the upload and continue-here doors on both transports.
// Under dev's 2026-10-21 contract amendment, history travels sealed; only the
// non-secret file is committed here so Git does not independently carry a key.
func TestC9RelayReceivesNoPlaintextSecretsAndTakeUsesTheVault(t *testing.T) {
	for _, transport := range []string{"spawn", "daemon"} {
		t.Run(transport, func(t *testing.T) {
			key := "sk-or-v1-0123456789abcdef0123456789abcdef"
			var leaked, uploaded atomic.Bool
			observe := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					raw, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					if bytes.Contains(raw, []byte(key)) {
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
				dir, err := os.MkdirTemp("", "safe-daemon-")
				if err != nil {
					t.Fatal(err)
				}
				d := cellstore.Daemon{Binary: h.bin, Socket: filepath.Join(dir, "e.sock")}
				t.Cleanup(func() { _ = d.Stop(context.Background()); _ = os.RemoveAll(dir) })
				h.engine.Transport = d
				h.engB.Transport = d
			}
			for path, content := range map[string]string{".env": "API_KEY=" + key + "\n", "config.py": "API = '" + key + "'\n", "main.py": "print('safe')\n"} {
				if err := os.WriteFile(filepath.Join(h.work, path), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			git(t, h.work, "init", "-q")
			git(t, h.work, "add", "main.py")
			git(t, h.work, "commit", "-qm", "fixture with untracked secrets")
			a := h.openA()
			a.mustSay("saved a local change")
			a.mustSay("saved another local change")
			h.durable(h.cell.ID, h.cell)
			if err := a.drive.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			// Fetch the chat alone first. No vault request can restore a key here.
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
			for _, path := range []string{".env", "config.py"} {
				if _, err := os.Lstat(filepath.Join(work, path)); !os.IsNotExist(err) {
					t.Fatalf("chat objects alone restored %s: %v", path, err)
				}
			}
			if got := gitOut(t, work, "log", "-1", "--format=%s"); got != "fixture with untracked secrets" {
				t.Fatalf("chat objects did not carry Git history: %q", got)
			}
			t.Setenv(home.EnvVar, h.homeB)
			h.engB.Guard.Home = h.homeB
			got, err := h.continuerB().Take(context.Background(), h.cell.ID)
			if err != nil {
				t.Fatal(err)
			}
			restored := workspaceOf(got.Taken.Cell.Root)
			for path, want := range map[string]string{".env": "API_KEY=" + key + "\n", "config.py": "API = '" + key + "'\n"} {
				if raw, err := os.ReadFile(filepath.Join(restored, path)); err != nil || string(raw) != want {
					t.Fatalf("vault restored %s as %q, %v", path, raw, err)
				}
			}
			if !uploaded.Load() || leaked.Load() {
				t.Fatalf("relay upload observed %v, plaintext secret received %v", uploaded.Load(), leaked.Load())
			}
		})
	}
}
