package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestIdleRelease_R8_AppRenameReachesReceiver(t *testing.T) {
	f := newIdleAppContract(t)
	first := f.released(t)
	chosen := "Kept after release"
	view, err := f.b.Rename(f.agent.ID, chosen)
	if err != nil || view.Title != chosen {
		t.Fatalf("rename=%+v error=%v", view, err)
	}
	appSyncWait(t, "app rename to save under a new released fence", func() bool {
		v, err := f.own.Dir.Cell(t.Context(), f.agent.ID)
		return err == nil && v.Cell.Lease.Fence > first.Cell.Lease.Fence && v.Cell.Lease.Expires == 0
	})
	peer := appDeviceSync(t, f.peerHome)
	engine := cellstore.EngineFor("")
	engine.DataRoot = filepath.Join(peer.Home, "v3", "stores")
	got, err := peer.Continuer(engine, syncsetup.TakeOptions{RootFor: func(id string) string { return takeRootForAt(peer.Home, id) }}).OpenAvailable(t.Context(), f.agent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := session.LoadMeta(got.Taken.Cell.Root)
	if err != nil || meta.Title != chosen {
		t.Fatalf("received title=%q error=%v", meta.Title, err)
	}
	raw, err := os.ReadFile(session.Place{Dir: got.Taken.Cell.Root}.Transcript())
	if err != nil || !strings.Contains(string(raw), chosen) {
		t.Fatalf("received journal=%s error=%v", raw, err)
	}
	if _, err := f.b.Rename(f.agent.ID, "refused words"); err == nil {
		t.Fatal("app acknowledged a rename after the peer took the chat")
	}
}
