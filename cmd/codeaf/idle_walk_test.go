package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func TestIdleRelease_C3_D1_AppModelSendPreservesIgnoredInstall(t *testing.T) {
	f := newIdleAppContract(t)
	root := session.DirOf(appTranscriptOf(f.b, f.agent.ID))
	meta, err := session.LoadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	work := meta.Workspace
	if meta.Owned {
		work = session.Place{Dir: root, Owned: true}.Work()
	}
	ignored := filepath.Join(work, "node_modules", "x", "index.js")
	if raw, err := exec.Command("git", "init", "--quiet", work).CombinedOutput(); err != nil {
		t.Fatalf("fixture repository: %s, %v", raw, err)
	}
	if err := os.MkdirAll(filepath.Dir(ignored), 0700); err != nil {
		t.Fatal(err)
	}
	for file, text := range map[string]string{filepath.Join(work, ".gitignore"): "node_modules/\n", ignored: "local install"} {
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	seed, err := f.b.Send(f.agent.ID, "seal the ignored-install fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, f.rec, seed)
	f.released(t)
	if raw, err := os.ReadFile(ignored); err != nil || string(raw) != "local install" {
		journal, _ := os.ReadFile(appTranscriptOf(f.b, f.agent.ID))
		t.Fatalf("fixture ignored file: %q, %v; journal:\n%s", raw, err, journal)
	}
	f.rec.mu.Lock()
	before := len(f.rec.data)
	f.rec.mu.Unlock()
	live := f.b.agent(f.agent.ID).live
	turn, err := f.b.Send(f.agent.ID, "ordinary message after release", "")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, f.rec, turn)
	if f.b.agent(f.agent.ID).live != live {
		t.Fatal("D1 replaced the open app conversation")
	}
	if raw, err := os.ReadFile(ignored); err != nil || string(raw) != "local install" {
		t.Fatalf("D1 changed ignored local file: %q, %v", raw, err)
	}
	f.rec.mu.Lock()
	defer f.rec.mu.Unlock()
	raw, err := json.Marshal(f.rec.data[before:])
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Moved here", "Set this machine up like", "This chat moved here"} {
		if strings.Contains(string(raw), text) {
			t.Fatalf("D1 emitted move/setup text: %s", text)
		}
	}
}

func TestIdleOpen_C6_D2_AppExistingViewerPullsNewestHead(t *testing.T) {
	f := newIdleAppContract(t)
	seen := f.released(t)
	peer := appDeviceSync(t, f.peerHome)
	receiverEngine := cellstore.EngineFor("")
	receiverEngine.DataRoot = filepath.Join(peer.Home, "v3", "stores")
	receiver := peer.Continuer(receiverEngine, syncsetup.TakeOptions{
		RootFor: func(id string) string { return takeRootForAt(peer.Home, id) },
	})
	got, err := receiver.OpenAvailable(t.Context(), f.agent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceAgent := f.b.agent(f.agent.ID)
	appSyncWait(t, "source viewer", func() bool {
		viewer := false
		f.b.locked(func() { view := sourceAgent.view; viewer = view.Location != nil && !view.Location.Here })
		return viewer
	})
	root := got.Taken.Cell.Root
	meta, err := session.LoadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	work := meta.Workspace
	if meta.Owned {
		work = session.Place{Dir: root, Owned: true}.Work()
	}
	journal, err := os.OpenFile(session.Place{Dir: root}.Transcript(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fmt.Fprintln(journal, `{"type":"message","role":"assistant","content":"newest D2 journal from B"}`)
	closeErr := journal.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("journal: %v, %v", writeErr, closeErr)
	}
	engine := cellstore.EngineFor(work)
	engine.DataRoot = receiverEngine.DataRoot
	drive, err := peer.Drive(t.Context(), engine, got.Taken.Cell, syncsetup.DriveOptions{DeviceName: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, sealErr := drive.Store(engine).Seal(t.Context(), got.Taken.Cell, cellstore.TurnInfo{})
	closeErr = drive.Close(context.Background())
	if sealErr != nil || closeErr != nil {
		t.Fatalf("publish: %v, %v", sealErr, closeErr)
	}
	appSyncWait(t, "free newest row", func() bool {
		rows, err := f.b.ListChats()
		if err != nil {
			return false
		}
		for _, r := range rows.Chats {
			if r.Cell == f.agent.ID {
				return r.CanOpen && !r.Here && r.Seen.Fence == got.Taken.Driving.Fence
			}
		}
		return false
	})
	opened, err := f.b.OpenAgent(f.agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Location == nil || !opened.Location.Here {
		t.Fatalf("D2 viewer did not open here: %+v", opened)
	}
	raw, err := os.ReadFile(appTranscriptOf(f.b, f.agent.ID))
	if err != nil || !strings.Contains(string(raw), "newest D2 journal from B") {
		t.Fatalf("D2 retained stale journal: %q, %v", raw, err)
	}
	view, err := f.own.Dir.Cell(t.Context(), f.agent.ID)
	if err != nil || view.Cell.Lease.Device != f.own.Device.ID() || view.Cell.Lease.Fence <= seen.Cell.Lease.Fence || view.Cell.Head != sealed.Turn.ID {
		t.Fatalf("D2 receiver hold=%+v, %v", view, err)
	}
}
