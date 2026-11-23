package syncsetup

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// THE FIRST LAUNCH AFTER A PAIRING KNOWS IT HAS ANOTHER COMPUTER. A computer
// learns of the others from a roster read, and the first launch after
// `codeaf pair` has not read one yet, so blind pass-through answered "nobody to
// send to" on exactly the launch where a person looks for it. These tests pin
// the contract by what a person can observe on that launch, not by the files
// that carry it.

// forgetRoster makes a rig computer look the way it does moments after its
// pairing finished: paired, storage closed, no roster ever read.
func forgetRoster(t *testing.T, home string, s *Sync) {
	t.Helper()
	mustNil(t, os.Remove(othersPath(home, deviceBindingFor(s.Relay, s.Identity, s.Device))))
	if LiveTransferForHome(home) || s.OthersPaired() {
		t.Fatal("the rig could not make a computer that has read no roster")
	}
}

// Contract 1: whenever pass-through will carry chats on a computer paired
// moments ago, it does so from the first launch, with no roster read: the home
// reports it, the Sync reports it, and a chat's drive starts instead of being
// refused for closed storage.
func TestFirstLaunchAfterPairingCarriesChatsBeforeAnyRosterIsRead(t *testing.T) {
	r := newLiveRig(t)
	forgetRoster(t, r.homeA, r.a)
	_, err := r.a.Drive(context.Background(), r.engine, r.cell, DriveOptions{DeviceName: nameA})
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("the rig's computer was not at the point before the fix: Drive = %v", err)
	}

	mustNil(t, NotePaired(r.homeA, r.a.Relay))

	if !LiveTransferForHome(r.homeA) {
		t.Fatal("the home does not report blind pass-through moments after its pairing")
	}
	if !r.a.OthersPaired() || !r.a.LiveTransfer() || r.a.ChatStorageOff() {
		t.Fatalf("a computer paired moments ago: others=%v carries=%v chatStorageOff=%v", r.a.OthersPaired(), r.a.LiveTransfer(), r.a.ChatStorageOff())
	}
	d, err := r.a.Drive(context.Background(), r.engine, r.cell, DriveOptions{DeviceName: nameA})
	if err != nil {
		t.Fatalf("a chat on a computer paired moments ago is refused: %v", err)
	}
	mustNil(t, d.Close(context.Background()))
}

// Contract 3: nothing changes where pass-through does not carry chats. Each row
// is a computer whose pairing just finished and whose answer must stay "no".
func TestFirstLaunchAfterPairingChangesNothingWherePassThroughDoesNotApply(t *testing.T) {
	t.Run("the switch is off", func(t *testing.T) {
		r := newLiveRig(t)
		forgetRoster(t, r.homeA, r.a)
		t.Setenv(LiveTransferVar, "off")
		mustNil(t, NotePaired(r.homeA, r.a.Relay))
		if LiveTransferForHome(r.homeA) || r.a.LiveTransfer() || !r.a.ChatStorageOff() {
			t.Fatal("blind pass-through carries chats with the switch off")
		}
	})
	t.Run("storage is approved", func(t *testing.T) {
		r := newLiveRig(t)
		forgetRoster(t, r.homeA, r.a)
		r.approveStorage()
		mustNil(t, NotePaired(r.homeA, r.a.Relay))
		if LiveTransferForHome(r.homeA) || r.a.LiveTransfer() || r.a.ChatStorageOff() {
			t.Fatal("blind pass-through carries chats on an account whose storage is approved, or storage is reported off")
		}
	})
	t.Run("the relay does not gate storage", func(t *testing.T) {
		r := newLiveRig(t)
		forgetRoster(t, r.homeA, r.a)
		mustNil(t, SaveStorage(r.homeA, r.a.Relay, r.a.Identity.ID(), ""))
		mustNil(t, NotePaired(r.homeA, r.a.Relay))
		if LiveTransferForHome(r.homeA) || r.a.LiveTransfer() {
			t.Fatal("blind pass-through carries chats through a relay that stores them anyway")
		}
	})
	t.Run("the pairing went through another relay", func(t *testing.T) {
		r := newLiveRig(t)
		forgetRoster(t, r.homeA, r.a)
		mustNil(t, NotePaired(r.homeA, "https://another-relay.example"))
		if LiveTransferForHome(r.homeA) || r.a.OthersPaired() {
			t.Fatal("a pairing through one relay made another computer known on a different relay")
		}
	})
	t.Run("nothing was paired", func(t *testing.T) {
		r := newLiveRig(t)
		forgetRoster(t, r.homeA, r.a)
		if LiveTransferForHome(r.homeA) || r.a.LiveTransfer() || !r.a.ChatStorageOff() {
			t.Fatal("a computer nobody paired with carries chats")
		}
	})
}

// The pairing's word is only the first word: the next complete roster is the
// truth, so a computer whose only other was removed stops carrying chats and
// says the old sentence again, and one that kept a computer goes on.
func TestARosterReadAfterPairingHasTheLastWord(t *testing.T) {
	r := newLiveRig(t)
	forgetRoster(t, r.homeA, r.a)
	mustNil(t, NotePaired(r.homeA, r.a.Relay))
	binding := deviceBindingFor(r.a.Relay, r.a.Identity, r.a.Device)
	self := r.a.Device.ID()

	rememberOthers(r.homeA, binding, map[string]directory.Device{self: {}, "laptop": {}})
	if !LiveTransferForHome(r.homeA) {
		t.Fatal("a roster that lists the other computer ended pass-through")
	}
	rememberOthers(r.homeA, binding, map[string]directory.Device{self: {}, "laptop": {Revoked: true}})
	if LiveTransferForHome(r.homeA) || r.a.OthersPaired() {
		t.Fatal("a roster that lists the other computer as removed left pass-through on")
	}
}

// Contract 2, the half that lives here: something outside is told the moment
// the answer to "is another computer paired" changes, in either direction, and
// only then. A read that changes nothing says nothing.
func TestOthersChangeIsToldWhenTheAnswerChanges(t *testing.T) {
	home := t.TempDir()
	here := deviceBinding{relay: "https://relay.example", identity: "id-1", self: "this-mac"}
	var mu sync.Mutex
	told := 0
	stop := OnOthersChange(func(h string) {
		if h != home {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		told++
	})
	defer stop()
	count := func() int { mu.Lock(); defer mu.Unlock(); return told }
	step := func(name string, devices map[string]directory.Device, want int) {
		t.Helper()
		rememberOthers(home, here, devices)
		if got := count(); got != want {
			t.Fatalf("%s: told %d times, want %d", name, got, want)
		}
	}
	step("a roster of this computer alone", map[string]directory.Device{"this-mac": {}}, 0)
	step("a second computer appears", map[string]directory.Device{"this-mac": {}, "laptop": {}}, 1)
	step("the same roster again", map[string]directory.Device{"this-mac": {}, "laptop": {}}, 1)
	step("the second computer is removed", map[string]directory.Device{"this-mac": {}, "laptop": {Revoked: true}}, 2)
	step("the removal again", map[string]directory.Device{"this-mac": {}, "laptop": {Revoked: true}}, 2)

	mustNil(t, os.RemoveAll(othersPath(home, here)))
	stop()
	rememberOthers(home, here, map[string]directory.Device{"this-mac": {}, "laptop": {}})
	if got := count(); got != 2 {
		t.Fatalf("a stopped listener was told: %d", got)
	}
}

// unsharedLocalChat is a chat on disk with a saved turn that no directory
// record names yet: what the terminal's background publisher offers.
func unsharedLocalChat(t *testing.T, s *Sync) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(filepath.Join(s.Home, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	mustNil(t, err)
	work := filepath.Join(c.Root, "work")
	mustNil(t, os.MkdirAll(work, 0700))
	// Larger than the export size a publisher asks the store about before it
	// sends (cellsync.askFloor), as a real project folder is.
	body := make([]byte, 256<<10)
	_, err = rand.Read(body)
	mustNil(t, err)
	mustNil(t, os.WriteFile(filepath.Join(work, "notes.bin"), body, 0600))
	mustNil(t, session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "an older chat", Workspace: work, Owned: true, Created: time.Now()}))
	appendTo(t, transcriptOf(c), `{"type":"session","version":3,"id":"`+c.ID+`"}`+"\n")
	appendTo(t, transcriptOf(c), `{"type":"message","role":"user","content":"a saved message"}`+"\n")
	engine := cellstore.EngineFor(work)
	engine.DataRoot = filepath.Join(s.Home, "v3", "stores")
	_, err = engine.Seal(t.Context(), c, cellstore.TurnInfo{})
	mustNil(t, err)
	mustNil(t, cellstore.Await(t.Context(), c))
	return c
}

// NOTHING IS SENT TO THE RELAY'S STORE WHILE PASS-THROUGH CARRIES CHATS. The
// hand test of a brand-new identity on the hosted dev relay found one 8 MB
// frame put, and thirteen existence checks, in a chat's stats before its first
// publish into the live store: the terminal's background publisher offers the
// chats the directory does not list yet, and it uploaded to the relay's store
// whatever the account's storage said. The relay refuses the frame, but the
// chat's content had already been sent. This runs the whole first launch of a
// computer paired moments ago (the pairing, a window's chat published live, the
// background pass over an older chat) and lists what reached the relay's store.
func TestLiveTransferNothingReachesTheRelaysStoreFromLaunchThroughTheFirstPublish(t *testing.T) {
	r := newLiveRig(t)
	forgetRoster(t, r.homeA, r.a)
	mustNil(t, NotePaired(r.homeA, r.a.Relay))
	older := unsharedLocalChat(t, r.a)

	a := liveChat(t, r)
	defer a.drive.Close(context.Background())
	// The background publisher's passes, on the schedule the terminal runs them.
	at := time.Now()
	var failed error
	for pass := 0; pass < 3; pass++ {
		failed = errors.Join(failed, r.a.PublishLocalChats(t.Context(), func() time.Time { return at }, nil))
		at = at.Add(2 * SharingRetryMax)
	}

	var reached []string
	for _, e := range r.wire.snapshot() {
		if strings.HasPrefix(e.Path, "/v1/store/") && e.Path != "/v1/store/stats" {
			reached = append(reached, fmt.Sprintf("%s %s %d bytes", e.Method, e.Path, e.Bytes))
		}
	}
	if len(reached) > 0 {
		t.Fatalf("%d requests for the relay's store went out while pass-through carries chats:\n  %s", len(reached), strings.Join(reached, "\n  "))
	}
	if n := r.wire.storeWrites(); n != 0 {
		t.Fatalf("%d object writes reached the relay", n)
	}
	// A pass that has nothing it may send is not a failure: nothing was refused,
	// so nothing is retried with a backoff or said to the person.
	if failed != nil {
		t.Fatalf("the background publisher reports a failure for chats it had no road for: %v", failed)
	}
	if _, err := r.a.Dir.Cell(t.Context(), older.ID); err == nil {
		t.Fatal("the older chat was listed in the directory by a computer that has nothing to carry it with")
	}
}

// The same pass is exactly what it was where the relay stores: with the
// account's storage approved, the background publisher offers an older chat to
// the relay's store, and the directory lists it.
func TestTheBackgroundPublisherStillOffersChatsOnceStorageIsApproved(t *testing.T) {
	r := newLiveRig(t)
	older := unsharedLocalChat(t, r.a)
	r.approveStorage()
	if err := r.a.PublishLocalChats(t.Context(), time.Now, nil); err != nil {
		t.Fatalf("the pass failed with storage approved: %v", err)
	}
	if r.wire.storeWrites() == 0 {
		t.Fatal("storage is approved and the relay was sent nothing")
	}
	if _, err := r.a.Dir.Cell(t.Context(), older.ID); err != nil {
		t.Fatalf("the older chat is not listed after the pass: %v", err)
	}
}
