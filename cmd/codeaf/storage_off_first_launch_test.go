package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// A COMPUTER PAIRED MOMENTS AGO IS TOLD BY ITS PAIRING THAT IT HAS ANOTHER. The
// first launch after `codeaf pair` has read no roster, and until it has, blind
// pass-through answered "nobody to send to" and the start page said chats stay
// on this computer. Both computers of a pairing, by every road a pairing takes,
// know of the other the moment it finishes.

// carriesChatsAfterPairing says whether blind pass-through would carry the
// chats of home right now, on a hosted-like relay whose storage is closed, with
// nothing read from the relay since the pairing.
func carriesChatsAfterPairing(t *testing.T, home, relay string) bool {
	t.Helper()
	id, err := identity.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(home, relay, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	return syncsetup.LiveTransferForHome(home)
}

func TestAPairingByCodeLeavesBothComputersKnowingOfTheOther(t *testing.T) {
	t.Setenv(syncsetup.LiveTransferVar, "")
	rig := newPairRig(t)
	t.Setenv(syncsetup.URLVar, rig.url)

	shown := rig.show(t)
	code := shown.code(t)
	joined := make(chan error, 1)
	go func() { _, err := rig.joinWith(t, code, false); joined <- err }()
	shown.out.waitFor(t, `wants your chats`)
	if _, err := io.WriteString(shown.typed, "y\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-joined; err != nil {
		t.Fatal(err)
	}
	if err := shown.end(t); err != nil {
		t.Fatal(err)
	}

	if !carriesChatsAfterPairing(t, rig.homeB, rig.url) {
		t.Error("the computer that typed the code does not know of the one that showed it")
	}
	if !carriesChatsAfterPairing(t, rig.homeA, rig.url) {
		t.Error("the computer that showed the code does not know of the one that typed it")
	}
}

func TestAPairingByLinkLeavesBothComputersKnowingOfTheOther(t *testing.T) {
	t.Setenv(syncsetup.LiveTransferVar, "")
	rig := newPairRig(t)
	t.Setenv(syncsetup.URLVar, rig.url)

	screen, done := rig.asking(t)
	typed := screen.waitFor(t, `codeaf pair approve (\S+)`)[1]
	if _, err := rig.approveWith(t, []string{"approve", typed}, "a\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if !carriesChatsAfterPairing(t, rig.homeB, rig.url) {
		t.Error("the computer that asked to join does not know of the one that approved it")
	}
	if !carriesChatsAfterPairing(t, rig.homeA, rig.url) {
		t.Error("the computer that approved does not know of the one that asked")
	}
}

// A pairing that was not finished tells nobody anything: a computer that only
// showed a code and was never answered has nobody to send a chat to.
func TestACodeNobodyTypedLeavesTheComputerKnowingOfNobody(t *testing.T) {
	t.Setenv(syncsetup.LiveTransferVar, "")
	rig := newPairRig(t)
	t.Setenv(syncsetup.URLVar, rig.url)

	shown := rig.show(t)
	shown.code(t)
	shown.stop()
	_ = shown.end(t)

	if carriesChatsAfterPairing(t, rig.homeA, rig.url) {
		t.Error("a code nobody typed made another computer known")
	}
}

// CONTRACT 2: the storage-off sentence that was shown is replaced, without a
// restart, when the roster (or a pairing) shows another computer after it was
// shown, and the old one comes back when the last other is gone. The wiring is
// the door's: syncsetup tells it, and it says the new sentence on the notice
// desk and in the replay each chat keeps for a seat that opens later.
func TestTheStorageOffSentenceShownIsReplacedWhenAnotherComputerBecomesKnown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	t.Setenv(syncsetup.LiveTransferVar, "")
	t.Setenv(syncsetup.URLVar, "https://relay.example")
	id, err := identity.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.EndSolo(dir); err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(dir, "https://relay.example", id.ID(), "off"); err != nil {
		t.Fatal(err)
	}

	said := make(chan string, 4)
	was := storageOffAgain
	storageOffAgain = func(line string) { said <- line }
	t.Cleanup(func() { storageOffAgain = was })
	nothingSaid := func(why string) {
		t.Helper()
		select {
		case line := <-said:
			t.Fatalf("%s: said %q", why, line)
		case <-time.After(150 * time.Millisecond):
		}
	}

	// The old sentence is shown: no other computer is known yet.
	if got := chatlist.StorageOffSentence(); got != chatlist.RelayStorageOff {
		t.Fatalf("before any roster the sentence is %q", got)
	}
	// A roster (here, the pairing's own word) shows another computer.
	if err := syncsetup.NotePaired(dir, "https://relay.example"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-said:
		if line != chatlist.RelayStorageOffLive {
			t.Fatalf("the shown sentence was replaced by %q, want the pass-through one", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the sentence on screen was never replaced")
	}
	nothingSaid("a second read that changes nothing")
	if err := syncsetup.NotePaired(dir, "https://relay.example"); err != nil {
		t.Fatal(err)
	}
	nothingSaid("the same news twice")

	// Where the account's storage is approved there is no storage-off sentence
	// to replace, whatever the roster says.
	id2, _ := identity.Load(dir)
	if err := syncsetup.SaveStorage(dir, "https://relay.example", id2.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	resayStorageOff(dir)
	nothingSaid("storage is on")
}

// The reverse, and what is not this home's: with the other computer gone the old
// sentence replaces the pass-through one; another home's change says nothing.
func TestTheOldStorageOffSentenceReturnsWhenTheLastOtherComputerIsGone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	t.Setenv(syncsetup.URLVar, "https://relay.example")
	id, err := identity.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(dir, "https://relay.example", id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	live := true
	was, wasLive := storageOffAgain, chatlist.LiveCarriesChats
	var said []string
	storageOffAgain = func(line string) { said = append(said, line) }
	chatlist.LiveCarriesChats = func() bool { return live }
	t.Cleanup(func() { storageOffAgain, chatlist.LiveCarriesChats = was, wasLive })

	if got := chatlist.StorageOffSentence(); got != chatlist.RelayStorageOffLive {
		t.Fatalf("setup: %q", got)
	}
	live = false
	resayStorageOff(t.TempDir())
	if len(said) != 0 {
		t.Fatalf("another home's change replaced this one's sentence: %q", said)
	}
	resayStorageOff(dir)
	if len(said) != 1 || said[0] != chatlist.RelayStorageOff {
		t.Fatalf("after the last other computer was removed: %q; want the old sentence", said)
	}
}

// A chat that opens after the sentence changed is shown the sentence that is
// true, not the one the book kept: the replay follows the replacement, and only
// for a storage-off sentence.
func TestTheReplayAChatKeepsFollowsTheReplacementSentence(t *testing.T) {
	storageOff := &driveNotices{last: chatlist.RelayStorageOff}
	other := &driveNotices{last: chatlist.Unreachable}
	none := &driveNotices{}
	book := &driveBook{drive: map[string]*liveDrive{
		"a": {notices: storageOff}, "b": {notices: other}, "c": {notices: none}, "d": {},
	}}
	book.restateStorageOff(chatlist.RelayStorageOffLive)
	if storageOff.last != chatlist.RelayStorageOffLive {
		t.Errorf("a kept storage-off sentence is %q", storageOff.last)
	}
	if other.last != chatlist.Unreachable || none.last != "" {
		t.Errorf("a sentence that is not about storage changed: %q, %q", other.last, none.last)
	}
	if !strings.Contains(chatlist.RelayStorageOffLive, "pass-through") {
		t.Fatal("setup: the replacement is not the pass-through sentence")
	}
}
