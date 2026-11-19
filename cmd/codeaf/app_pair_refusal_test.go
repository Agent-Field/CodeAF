package main

// A joining computer that will not keep what it is sent is told so before the
// other computer is asked, wherever this computer can know it; where it cannot,
// the other computer still never says the two are paired.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// pairedHomes makes homes that share one identity, each registered at the relay
// under its name: computers that are paired with each other.
func pairedHomes(t *testing.T, names ...string) ([]string, []*syncsetup.Sync) {
	t.Helper()
	dirs := make([]string, len(names))
	syncs := make([]*syncsetup.Sync, len(names))
	var id identity.Identity
	for i, name := range names {
		dirs[i] = t.TempDir()
		var err error
		if i == 0 {
			id, err = identity.Ensure(dirs[i])
		} else {
			_, err = identity.Adopt(dirs[i], id, false)
		}
		if err != nil {
			t.Fatal(err)
		}
		// The name is kept on disk too, so a running engine republishes it and
		// not the host's name.
		if _, err := devname.Set(dirs[i], name); err != nil {
			t.Fatal(err)
		}
		syncs[i] = appDeviceSync(t, dirs[i])
		if err := syncs[i].Rename(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	return dirs, syncs
}

// chatOfItsOwn gives a home one chat with a turn in it.
func chatOfItsOwn(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, "v3", "projects", "project", "mine", ".cell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(`{"type":"message","role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// offerAndUse has A show a code and B enter it, and answers both operations' ids.
func offerAndUse(t *testing.T, wa, wb *actionWire) (offer, join string) {
	t.Helper()
	offer = machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "offer_code", Name: "A"}).Pairing.ID
	waiting := e2PairWait(t, wa, offer, "waiting")
	join = machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: waiting.Code, Name: "B"}).Pairing.ID
	return offer, join
}

// pairStates is every state the operation id passed through, in order.
func pairStates(rec *appRecorder, id string) []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var states []string
	for i, name := range rec.events {
		if name != appwire.EventPairChanged {
			continue
		}
		if p := rec.data[i].(appwire.PairResult).Pairing; p.ID == id {
			states = append(states, p.State)
		}
	}
	return states
}

// syncModeBefore is the sync mode the app was last told before the operation id
// reached state, or "" when it was told none.
func syncModeBefore(rec *appRecorder, id, state string) string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	mode := ""
	for i, name := range rec.events {
		switch name {
		case appwire.EventSyncChanged:
			mode = rec.data[i].(appwire.SyncChanged).Sync.Mode
		case appwire.EventPairChanged:
			if p := rec.data[i].(appwire.PairResult).Pairing; p.ID == id && p.State == state {
				return mode
			}
		}
	}
	return ""
}

// wantCannotJoin is B's refusal: failed, with the app's sentence and the code
// that tells the app a new code would not help.
func wantCannotJoin(t *testing.T, failed appwire.Pairing, message string) {
	t.Helper()
	if failed.Error == nil || failed.Error.Code != appwire.CodeCannotJoin || failed.Error.Message != message {
		t.Fatalf("B ended with %+v, want cannot_join: %q", failed.Error, message)
	}
}

// A COMPUTER WITH ITS OWN IDENTITY AND A CHAT, AND NO OTHER COMPUTER IN THAT
// IDENTITY, IS REFUSED BEFORE THE OTHER COMPUTER IS ASKED. It is not solo (an
// older build ended the marker when it showed a code, or the computers it shared
// with were removed); the directory says nothing else holds its chats. The
// computer that showed the code shared nothing, so it is still solo.
func TestAppPairOwnChatsRefusedBeforeApproval(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirs, _ := pairedHomes(t, "B")
	chatOfItsOwn(t, dirs[0])
	dirA := t.TempDir()
	_, recA, wa := appDeviceBackend(t, dirA)
	_, recB, wb := appDeviceBackend(t, dirs[0])
	defer assertAppPairCopyM9(t, recA)
	defer assertAppPairCopyM9(t, recB)

	offer, join := offerAndUse(t, wa, wb)
	failed := e2PairWait(t, wb, join, "failed")
	wantCannotJoin(t, failed, "This computer has 1 chat of its own, so it can't join another computer's chats. Nothing was changed.")
	if states := pairStates(recA, offer); strings.Contains(strings.Join(states, " "), "question") || strings.Contains(strings.Join(states, " "), "done") {
		t.Fatalf("A went through %v for a computer that refused", states)
	}
	if got := appDeviceList(t, wa); len(got.Devices) != 1 {
		t.Fatalf("A lists %+v", got.Devices)
	}
	machineCall[appwire.PairResult](t, wa, "pair.cancel", appwire.IDParams{ID: offer})
	if !identity.Solo(dirA) || !syncsetup.Quiet(dirA) {
		t.Fatal("A's code shared nothing, yet A is no longer solo")
	}
}

// A COMPUTER THAT IS PAIRED WITH OTHERS IS REFUSED BEFORE THE OTHER COMPUTER IS
// ASKED, in the app's words: its chats sync elsewhere, and no replacement is
// offered, because replacing would set them aside.
func TestAppPairActivelyPairedRefusedBeforeApproval(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirs, _ := pairedHomes(t, "B", "C")
	dirA := t.TempDir()
	_, recA, wa := appDeviceBackend(t, dirA)
	_, recB, wb := appDeviceBackend(t, dirs[0])
	defer assertAppPairCopyM9(t, recA)
	defer assertAppPairCopyM9(t, recB)

	offer, join := offerAndUse(t, wa, wb)
	failed := e2PairWait(t, wb, join, "failed")
	wantCannotJoin(t, failed, "This computer is already paired with C.")
	if states := pairStates(recA, offer); strings.Contains(strings.Join(states, " "), "question") {
		t.Fatalf("A was asked to approve a computer that is paired elsewhere: %v", states)
	}
	machineCall[appwire.PairResult](t, wa, "pair.cancel", appwire.IDParams{ID: offer})
	if !identity.Solo(dirA) {
		t.Fatal("A's code shared nothing, yet A is no longer solo")
	}

	// A link is refused the same way, before anything is shown to approve.
	link := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "request_link", Name: "B"}).Pairing.ID
	wantCannotJoin(t, e2PairWait(t, wb, link, "failed"), "This computer is already paired with C.")
}

// A REFUSAL THAT ONLY THE EXCHANGE CAN FIND IS NOT A PAIRING ON EITHER SIDE. A
// computer removed from its own set keeps its chats and may pair back, so it is
// not refused before the code; offered someone else's chats, it refuses after A
// approved, and A says the pairing was not confirmed and to check B, not done.
func TestAppPairLateRefusalIsNotConfirmed(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirs, syncs := pairedHomes(t, "B", "C")
	if err := syncs[1].Dir.Revoke(context.Background(), syncs[0].Device.ID()); err != nil {
		t.Fatal(err)
	}
	chatOfItsOwn(t, dirs[0])
	held, err := identity.Load(dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	dirA := t.TempDir()
	_, recA, wa := appDeviceBackend(t, dirA)
	_, recB, wb := appDeviceBackend(t, dirs[0])
	defer assertAppPairCopyM9(t, recA)
	defer assertAppPairCopyM9(t, recB)

	offer, join := offerAndUse(t, wa, wb)
	q := e2PairWait(t, wa, offer, "question")
	machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer, Question: q.Question.ID, Option: "y"})
	wantCannotJoin(t, e2PairWait(t, wb, join, "failed"), "This computer has 1 chat of its own, so it can't join another computer's chats. Nothing was changed.")
	a := e2PairWait(t, wa, offer, "failed")
	if a.Error == nil || a.Error.Code != appwire.CodeEngine || !strings.HasPrefix(a.Error.Message, "Pairing was not confirmed by B") || !strings.HasSuffix(a.Error.Message, "Check that computer to see whether it joined.") {
		t.Fatalf("A ended with %+v", a.Error)
	}
	if states := pairStates(recA, offer); strings.Contains(strings.Join(states, " "), "done") {
		t.Fatalf("A reached done for a computer that refused: %v", states)
	}
	// A's identity left in the answer, so A syncs from now on: its app hears so,
	// and A puts its own record in its directory, where a computer that did keep
	// the identity (one whose word was lost) can find it.
	if identity.Solo(dirA) {
		t.Fatal("A's identity was sent, yet A still says it is solo")
	}
	if mode := syncModeBefore(recA, offer, "failed"); mode != appwire.SyncOn {
		t.Fatalf("A's app last heard sync %q before the failure, want %q", mode, appwire.SyncOn)
	}
	ownA, err := identity.Device(dirA)
	if err != nil {
		t.Fatal(err)
	}
	directoryA := appDeviceSync(t, dirA)
	appSyncWait(t, "A's own record after its identity left", func() bool {
		listing, err := directoryA.Dir.List(context.Background())
		_, listed := listing.Devices[ownA.ID()]
		return err == nil && listed
	})
	if now, err := identity.Load(dirs[0]); err != nil || now.ID() != held.ID() {
		t.Fatalf("B's identity changed (%v)", err)
	}
	if got := appDeviceList(t, wa); len(got.Devices) != 1 {
		t.Fatalf("A lists %+v", got.Devices)
	}
}

// A NO SHARES NOTHING. The computer that showed the code and declined stays solo
// and quiet, and the one that typed it is told it was declined.
func TestAppPairDeclinedCodeStaysSolo(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirA := t.TempDir()
	_, recA, wa := appDeviceBackend(t, dirA)
	_, recB, wb := appDeviceBackend(t, t.TempDir())
	defer assertAppPairCopyM9(t, recA)
	defer assertAppPairCopyM9(t, recB)

	offer, join := offerAndUse(t, wa, wb)
	q := e2PairWait(t, wa, offer, "question")
	machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer, Question: q.Question.ID, Option: "n"})
	for side, w := range map[string]*actionWire{"A": wa, "B": wb} {
		id := map[string]string{"A": offer, "B": join}[side]
		if p := e2PairWait(t, w, id, "cancelled"); !slices.Contains(p.Lines, appPairDeclined) {
			t.Fatalf("%s ended with %+v, want declined", side, p)
		}
	}
	if !identity.Solo(dirA) || !syncsetup.Quiet(dirA) {
		t.Fatal("a declined code ended A's solo state")
	}
}

// CANCEL AFTER YES IS NOT "NOTHING WAS PAIRED". Once A answered, its identity has
// gone out; a person who cancels while A waits for B's word is told the pairing
// was not confirmed and to check B, which may hold the chats.
func TestAppPairCancelAfterYesIsNotConfirmed(t *testing.T) {
	celltest.Available(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	// B refuses only after the exchange, so A is left waiting for its word.
	dirs, syncs := pairedHomes(t, "B", "C")
	if err := syncs[1].Dir.Revoke(context.Background(), syncs[0].Device.ID()); err != nil {
		t.Fatal(err)
	}
	chatOfItsOwn(t, dirs[0])
	_, recA, wa := appDeviceBackend(t, t.TempDir())
	_, _, wb := appDeviceBackend(t, dirs[0])
	defer assertAppPairCopyM9(t, recA)

	offer, join := offerAndUse(t, wa, wb)
	q := e2PairWait(t, wa, offer, "question")
	machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer, Question: q.Question.ID, Option: "y"})
	e2PairWait(t, wb, join, "failed")
	got := machineCall[appwire.PairResult](t, wa, "pair.cancel", appwire.IDParams{ID: offer}).Pairing
	if got.State != "failed" || got.Error == nil || !strings.HasPrefix(got.Error.Message, "Pairing was not confirmed by B") || slices.Contains(got.Lines, appPairCancelled) {
		t.Fatalf("a cancel after yes ended with %+v", got)
	}
}
