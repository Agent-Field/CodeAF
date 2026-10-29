package syncsetup

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// M1–M5 exercise real local history, encrypted export/import, the pairing
// mailbox, directory publication, outage/retry and Continue here both ways.
func TestM1M2M3M4M5PairMergeRealHomes(t *testing.T) {
	h := newTwoHomes(t)
	ctx := t.Context()
	a := h.openA()
	a.mustSay("A first")
	a.mustSay("A second")
	h.durable(h.cell.ID, h.cell)
	mustNil(t, a.drive.Close(ctx))
	beforeID, beforeDev := h.a.Identity.ID(), h.a.Device.ID()
	h.homeB = t.TempDir()
	old, err := identity.EnsureSolo(h.homeB)
	mustNil(t, err)
	v, err := keys.Open(h.homeB)
	mustNil(t, err)
	mustNil(t, v.Put("p/KEY", keys.Entry{Name: "KEY", Value: "kept", Scope: "p"}))
	mustNil(t, os.WriteFile(filepath.Join(h.homeB, "config.json"), []byte(`{"ui.icons":"ascii","machine.path":"local"}`), 0600))
	brain, err := store.Open(filepath.Join(h.homeB, "graph.db"))
	mustNil(t, err)
	memory, err := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "Remember this pairing preference."})
	mustNil(t, err)
	mustNil(t, brain.Close())
	c, err := cell.CreateIn(filepath.Join(h.homeB, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	mustNil(t, err)
	work := filepath.Join(c.Root, "work")
	mustNil(t, os.MkdirAll(work, 0700))
	mustNil(t, session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "B brought chat", Workspace: work, Owned: true, Created: time.Now()}))
	h.engB.DataRoot = filepath.Join(h.homeB, "v3", "stores")
	h.engB.Workspace = work
	appendTo(t, transcriptOf(c), `{"type":"session","version":3,"id":"`+c.ID+`"}`+"\n")
	for _, text := range []string{"B first", "B second", "B third"} {
		appendTo(t, transcriptOf(c), `{"type":"message","role":"assistant","content":"`+text+`"}`+"\n")
		_, err := h.engB.Seal(ctx, c, cellstore.TurnInfo{})
		mustNil(t, err)
		mustNil(t, cellstore.Await(ctx, c))
	}
	wantB, err := os.ReadFile(transcriptOf(c))
	mustNil(t, err)
	wantA, err := os.ReadFile(transcriptOf(h.cell))
	mustNil(t, err)
	route := pair.Mailbox{Box: pairbox.NewHTTP(h.url, http.DefaultClient), URL: h.url, Host: "loopback"}
	screen := pairScreen{codes: make(chan *pair.Code, 1)}
	done := make(chan error, 1)
	go func() {
		_, err := pair.Offer(ctx, route, pair.Grant{Identity: h.a.Identity}, screen)
		done <- err
	}()
	code := <-screen.codes
	_, err = pair.Join(ctx, route, pair.Joining{Home: h.homeB, Label: nameB}, code.Shown(), screen)
	mustNil(t, err)
	mustNil(t, <-done)
	h.b = openSync(t, h.homeB)
	mustNil(t, h.b.SyncState(ctx))
	mustNil(t, h.a.SyncState(ctx))
	for _, side := range []*Sync{h.a, h.b} {
		v, err := keys.Open(side.Home)
		mustNil(t, err)
		if got, err := v.Get("p/KEY"); err != nil || got.Value != "kept" {
			t.Fatalf("M1 merged saved key: %+v, %v", got, err)
		}
		prefs, err := config.ReadPreferences(side.Home)
		mustNil(t, err)
		if prefs[config.KeyIcons] != `"ascii"` {
			t.Fatalf("M1 merged preferences: %v", prefs)
		}
		brain, err := store.Open(filepath.Join(side.Home, "graph.db"))
		mustNil(t, err)
		got, found, err := brain.MemoryRecord(memory.ID)
		mustNil(t, brain.Close())
		if err != nil || !found || got.Text != memory.Text {
			t.Fatalf("M1 merged memory: %+v, %v, %v", got, found, err)
		}
	}
	if h.b.Ledger == cellstore.LedgerName(h.url, old.ID()) {
		t.Fatal("M2 reused the old publication ledger")
	}
	// Relay failure after the local switch cannot make the local history unreadable.
	block := &blocker{Store: h.b.Store}
	h.b.Store = block
	block.set(true)
	if err := h.b.PublishLocalChats(ctx, nil); err == nil {
		t.Fatal("M5 expected the interrupted upload to fail")
	}
	if got, _ := os.ReadFile(transcriptOf(c)); !bytes.Equal(got, wantB) {
		t.Fatal("M5 local history changed during outage")
	}
	// A new process retries from disk with the same chat and computer ids.
	h.b = openSync(t, h.homeB)
	beforePublish := relayCounts(t, h.b)
	mustNil(t, h.b.PublishLocalChats(ctx, nil))
	afterPublish := relayCounts(t, h.b)
	mustNil(t, h.b.PublishLocalChats(ctx, nil))
	afterRetry := relayCounts(t, h.b)
	if afterPublish.BytesIn <= beforePublish.BytesIn || afterRetry.BytesIn != afterPublish.BytesIn || afterRetry.Puts != afterPublish.Puts {
		t.Fatalf("M5 retry upload work: before=%+v published=%+v retry=%+v", beforePublish, afterPublish, afterRetry)
	}
	t.Logf("M5 publication: %d bytes, %d frame puts; completed retry: 0 bytes, 0 frame puts", afterPublish.BytesIn-beforePublish.BytesIn, afterPublish.Puts-beforePublish.Puts)
	for _, side := range []*Sync{h.a, h.b} {
		rows, err := side.Rows(ctx)
		mustNil(t, err)
		if len(rows) != 2 {
			t.Fatalf("M2/M5 inventory: %+v", rows)
		}
		for _, row := range rows {
			mine := side == h.a && row.Cell == h.cell.ID || side == h.b && row.Cell == c.ID
			if row.Mine != mine || row.DeviceID == "" {
				t.Fatalf("M2 ownership: %+v", row)
			}
		}
	}
	id, _ := identity.Load(h.homeA)
	dev, _ := identity.Device(h.homeA)
	if id.ID() != beforeID || dev.ID() != beforeDev {
		t.Fatal("M4 offering computer changed")
	}
	// Make A's project path unavailable to the receiving computer.
	tookA, err := h.continuerB().Take(ctx, h.cell.ID)
	mustNil(t, err)
	if got, _ := os.ReadFile(transcriptOf(tookA.Taken.Cell)); !bytes.Equal(got, wantA) {
		t.Fatal("M3 A-to-B history differs")
	}
	tookB, err := h.continuerA().Take(ctx, c.ID)
	mustNil(t, err)
	if got, _ := os.ReadFile(transcriptOf(tookB.Taken.Cell)); !bytes.Equal(got, wantB) {
		t.Fatal("M3 B-to-A history differs")
	}
}

func TestM5OpenResumesApprovedPairMerge(t *testing.T) {
	h := newTwoHomes(t)
	home := t.TempDir()
	_, err := identity.EnsureSolo(home)
	mustNil(t, err)
	route := pair.Mailbox{Box: pairbox.NewHTTP(h.url, http.DefaultClient), URL: h.url, Host: "loopback"}
	screen := pairScreen{codes: make(chan *pair.Code, 1)}
	done := make(chan error, 1)
	go func() {
		_, err := pair.Offer(t.Context(), route, pair.Grant{Identity: h.a.Identity, SyncURL: h.url}, screen)
		done <- err
	}()
	code := <-screen.codes
	stopped := errors.New("process stopped")
	_, err = pair.Join(t.Context(), route, pair.Joining{Home: home, Label: nameB, After: func(step string) error {
		if step == "installed" {
			return stopped
		}
		return nil
	}}, code.Shown(), screen)
	if !errors.Is(err, stopped) {
		t.Fatalf("M5 interruption: %v", err)
	}
	mustNil(t, <-done)
	s := openSync(t, home)
	if identity.Solo(home) || s.Identity.ID() != h.a.Identity.ID() || Resolve(home).URL != h.url {
		t.Fatal("M5 startup did not finish the approved switch")
	}
}
