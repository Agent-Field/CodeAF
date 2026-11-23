package main

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// L4's error envelope is observable through agents.take. The relay injects an
// install failure by making the copy parent unwritable just after acquisition.
func TestAppTakeInstallFailureMessageL4(t *testing.T) {
	celltest.RequireEngine(t)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("failure injection needs ordinary file permissions")
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	parent := filepath.Join(dirB, "v3", "projects", "copies")
	var injected atomic.Bool
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/acquire") {
				if err := os.Chmod(parent, 0500); err != nil {
					t.Errorf("inject install failure: %v", err)
				} else {
					injected.Store(true)
				}
			}
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) })
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_HOME", dirA)
	c, err := cell.CreateIn(filepath.Join(dirA, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err = session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "Source chat", Workspace: work}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	eng := cellstore.Engine{DataRoot: filepath.Join(dirA, "v3", "stores"), Workspace: work}
	if _, err = eng.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	a := appDeviceSync(t, dirA)
	if err = a.PublishCopy(t.Context(), eng, c, "Mac", "Source chat"); err != nil {
		t.Fatal(err)
	}
	_, _, w := appDeviceBackend(t, dirB)
	rows := machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{}).Chats
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	_, failure := w.call("agents.take", appwire.TakeParams{Cell: c.ID, Seen: rows[0].Seen})
	_ = os.Chmod(parent, 0700)
	if !injected.Load() || failure == nil {
		t.Fatalf("L4 injection=%v error=%+v", injected.Load(), failure)
	}
	after, err := a.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, admitted := a.Dir.Heartbeat(t.Context(), c.ID, directory.Beat{Fence: rows[0].Seen.Fence})
	t.Logf("L4 recovery evidence: before=%+v after=%+v source heartbeat=%v", rows[0].Seen, after.Cell.Lease, admitted)
	if failure.Code != appwire.CodeEngine || failure.Message != "This chat could not be moved to this computer." || !strings.Contains(string(e2JSON(failure.Detail)), "permission denied") {
		t.Fatalf("L4 error envelope: %+v", failure)
	}
}
