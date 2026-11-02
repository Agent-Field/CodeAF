package main

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// S5 uses L4's post-acquisition permission failure with a real open source
// conversation, then submits through the source app's ordinary send door.
func TestAppTakeInstallFailureS5(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("L4 injection needs ordinary file permissions")
	}
	celltest.RequireEngine(t)
	dirB := t.TempDir()
	parent := filepath.Join(dirB, "v3", "projects", "copies")
	var armed, injected atomic.Bool
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/acquire") && armed.CompareAndSwap(true, false) {
				if err := os.Chmod(parent, 0500); err != nil {
					t.Errorf("S5 injection: %v", err)
				} else {
					injected.Store(true)
				}
			}
		})
	})
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) })
	source, rec, _, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Setenv("CODEAF_MODEL", "test/model")
	dirA := home.Dir()
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cell.CreateIn(filepath.Join(dirA, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	private := (session.Place{Dir: c.Root, Owned: true}).Work()
	if err = os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err = session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "Source chat", Workspace: workspace, Owned: true, LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), []byte(`{"type":"message","role":"user","content":"seed"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	eng := cellstore.Engine{DataRoot: filepath.Join(dirA, "v3", "stores"), Workspace: private}
	if _, err = eng.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	owner := appDeviceSync(t, dirA)
	if err = owner.PublishCopy(t.Context(), eng, c, "Source S5", "Source chat"); err != nil {
		t.Fatal(err)
	}
	agent, err := source.OpenAgent(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	source.drives.mu.Lock()
	drive := source.drives.drive[c.ID]
	source.drives.mu.Unlock()
	if drive == nil {
		t.Fatal("S5 source has no drive")
	}
	if err = drive.Gate(); err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	_, _, wire := appDeviceBackend(t, dirB)
	rows := machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats
	if len(rows) != 1 {
		t.Fatalf("S5 rows before take: %+v", rows)
	}
	armed.Store(true)
	_, failure := wire.call("agents.take", appwire.TakeParams{Cell: c.ID, Seen: rows[0].Seen})
	_ = os.Chmod(parent, 0700)
	if !injected.Load() || failure == nil || failure.Message != appTakeFailed {
		t.Fatalf("S5 injection=%v failure=%+v", injected.Load(), failure)
	}
	after, err := owner.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Cell.Lease.Expires != 0 || after.Cell.Lease.Fence != rows[0].Seen.Fence+1 {
		t.Fatalf("S5 failed take lease: %+v", after.Cell.Lease)
	}
	rows = machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats
	if len(rows) != 1 || rows[0].Here || rows[0].LocalAgent != "" || !rows[0].CanTake || rows[0].Holder == nil || rows[0].Holder.ID != after.Cell.Lease.Device || strings.Contains(rows[0].Line, "open on") {
		t.Fatalf("S5 failed taker row: %+v", rows)
	}
	t.Setenv(home.EnvVar, dirA)
	if err = drive.started().CheckLease(t.Context()); err != nil {
		t.Fatal(err)
	}
	var view appwire.Agent
	source.locked(func() { view = source.agents[agent.ID].view })
	if view.Location != nil && strings.Contains(view.Location.Line, "continued") {
		t.Fatalf("S5 released chat claims a continuation: %+v", view.Location)
	}
	turn, err := source.Send(agent.ID, "continue after the failed move", "s5-recover")
	if err != nil {
		t.Fatalf("S5 source send: %v", err)
	}
	appTurnDoneFor(t, rec, turn)
	recovered, err := owner.Dir.Cell(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Cell.Lease.Device != owner.Device.ID() || recovered.Cell.Lease.Fence <= after.Cell.Lease.Fence || recovered.Cell.Lease.Expires <= recovered.Now {
		t.Fatalf("S5 source did not reacquire: %+v", recovered.Cell.Lease)
	}
}
