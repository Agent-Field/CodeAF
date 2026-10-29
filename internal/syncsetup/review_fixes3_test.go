package syncsetup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/session"
)

type r3SwitchAfterList struct {
	directory.Client
	switchHome func()
}

// A chat discovered before its first save stays pending without an error or
// retry delay, and its first head is shared during this process's lifetime.
func TestR3_3_UnsavedChatSharesAsSoonAsItHasAHead(t *testing.T) {
	r := newDriveRig(t)
	c, err := cell.CreateIn(filepath.Join(r.a.Home, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	mustNil(t, err)
	work := filepath.Join(c.Root, "work")
	mustNil(t, os.MkdirAll(work, 0700))
	mustNil(t, session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "first save", Workspace: work, Owned: true, Created: time.Now()}))
	at := time.Now()
	clock := func() time.Time { return at }
	notices := 0
	say := func(cell.Cell, string) { notices++ }
	for range 3 {
		mustNil(t, r.a.PublishLocalChats(t.Context(), clock, say))
		mustNil(t, r.a.RefreshLocalChats())
	}
	if notices != 0 {
		t.Fatal("a chat with nothing to share yet reported a failure")
	}
	if _, err := r.a.Dir.Cell(t.Context(), c.ID); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("unsaved chat was published: %v", err)
	}
	appendTo(t, transcriptOf(c), `{"type":"session","version":3,"id":"`+c.ID+`"}`+"\n")
	appendTo(t, transcriptOf(c), `{"type":"message","role":"user","content":"first saved message"}`+"\n")
	engine := cellstore.EngineFor(work)
	engine.DataRoot = filepath.Join(r.a.Home, "v3", "stores")
	_, err = engine.Seal(t.Context(), c, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, cellstore.Await(t.Context(), c))
	head, err := cellstore.Head(c)
	mustNil(t, err)
	if head == nil {
		t.Fatal("test's first save did not create a head")
	}
	// Do not advance time: nothing saved yet must not impose backoff.
	mustNil(t, r.a.PublishLocalChats(t.Context(), clock, say))
	view, err := r.a.Dir.Cell(t.Context(), c.ID)
	if err != nil || view.Cell.Head != head.Turn.ID {
		t.Fatalf("first saved head was not shared without restarting: head=%q, %v", view.Cell.Head, err)
	}
	if notices != 0 {
		t.Fatal("first save reported a publication failure")
	}
}

func (d *r3SwitchAfterList) List(ctx context.Context) (directory.Listing, error) {
	l, err := d.Client.List(ctx)
	if err == nil && d.switchHome != nil {
		switchHome := d.switchHome
		d.switchHome = nil
		switchHome()
	}
	return l, err
}

// An already-running state round must never upload the new set's saved keys
// through the clients bound to the set it just left.
func TestR3_1_StateRoundRefusesVaultFromNewPairing(t *testing.T) {
	for _, replace := range []bool{true, false} {
		t.Run(map[bool]string{true: "replace", false: "merge"}[replace], func(t *testing.T) {
			home := t.TempDir()
			old, err := identity.EnsureSolo(home)
			mustNil(t, err)
			if replace {
				mustNil(t, identity.EndSolo(home))
			}
			v, err := keys.Open(home)
			mustNil(t, err)
			mustNil(t, v.Put("before", keys.Entry{Name: "BEFORE", Value: "old set"}))
			oldDir := directory.NewMemory(time.Now).For("old-device")
			oldStore := blobstore.NewMemory()
			next, err := identity.Mint()
			mustNil(t, err)
			switchHome := func() {
				route := pair.Mailbox{Box: pairbox.NewMemory(pairbox.DefaultLimits, nil), Host: "relay.test"}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				screen := pairScreen{codes: make(chan *pair.Code, 1)}
				done := make(chan error, 1)
				go func() {
					_, err := pair.Offer(ctx, route, pair.Grant{Identity: next}, screen)
					done <- err
				}()
				select {
				case code := <-screen.codes:
					_, err := pair.Join(ctx, route, pair.Joining{Home: home, Replace: replace}, code.Shown(), screen)
					mustNil(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				mustNil(t, <-done)
				mustNil(t, v.Put("after", keys.Entry{Name: "AFTER", Value: "new set only"}))
			}
			dir := &r3SwitchAfterList{Client: oldDir, switchHome: switchHome}
			s := &Sync{Home: home, Identity: old, Dir: dir, Store: oldStore}
			if !replace {
				// A solo home is quiet until pairing ends. Its old sync handle
				// can still run before the worker notices the new binding.
				dir.switchHome = nil
				switchHome()
			}
			if err := s.SyncState(t.Context()); !errors.Is(err, keys.ErrOtherKey) {
				t.Errorf("round crossing pairing must refuse the changed key: %v", err)
			}
			listing, err := oldDir.List(t.Context())
			mustNil(t, err)
			if listing.Identity.Vault != "" || len(oldStore.Log()) != 0 {
				t.Errorf("new set's keys reached old set: vault=%q store operations=%+v", listing.Identity.Vault, oldStore.Log())
			}
			fresh := &Sync{Home: home, Identity: next, Dir: directory.NewMemory(time.Now).For("new-device"), Store: blobstore.NewMemory()}
			mustNil(t, fresh.SyncState(t.Context()))
			listing, err = fresh.Dir.List(t.Context())
			mustNil(t, err)
			if listing.Identity.Vault == "" {
				t.Fatal("next round did not share the saved keys under the new binding")
			}
			got, err := v.Get("after")
			if err != nil || got.Value != "new set only" {
				t.Fatalf("new saved key after retry = %+v, %v", got, err)
			}
		})
	}
}
