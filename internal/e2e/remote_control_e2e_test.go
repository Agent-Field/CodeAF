//go:build e2e

package e2e

// remote_control_e2e_test.go is THE KEYLESS PROOF OF REMOTE CONTROL, DRIVEN AS A
// PERSON WOULD DRIVE IT: the real binary in real terminals, two or three paired
// computers on one machine, a relay in the middle, and a model that is a script.
//
// A chat another of the person's computers is running opens LIVE: the other
// computer keeps running it, the person watches it happen, types into it, and
// can carry it to this computer with Move here. Every scenario below is one line
// of that promise, named for the contract it checks (the V ids are the lines of
// the validation contract in the remote control design).
//
// IT NEEDS NO PROVIDER KEY AND NO NETWORK. The model is an httptest server that
// answers by what the typed words say: a prompt with `slow` in it is held open
// until the test releases a gate, so a turn is RUNNING while a viewer attaches;
// any other prompt answers at once, with `host reply` or `laptop reply` according
// to the computer the words were typed on. The relay is the product's own, behind
// a proxy that writes down every request, refuses every store write with the
// answer the hosted service gives when storage is off, and fails the scenario if
// one was ever tried. Sync storage is off on every home, so what these scenarios
// prove is the live road alone: nothing a viewer does here may need an upload.
//
// IT SKIPS RATHER THAN FAILS without tmux or bin/codeaf, like every scenario in
// this package, and it is meant to be run ONE SUBTEST AT A TIME:
//
//	go test -tags e2e -count=1 -run 'TestRemoteControlE2E/<name>' -v ./internal/e2e/
//
// Every subtest builds its own homes, relay, proxy and model, so none depends on
// another having run. The first start of each window is slow; the waits say how
// long the contract allows and add only the margin a real clock needs.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// The names the scenarios give their computers. A device name is what a person
// reads on Home and in every banner, so these are the words the needles meet.
const (
	rcHostName   = "desktop"
	rcViewerName = "laptop"
	rcSecondName = "tablet"
)

// The words the scripted model answers with. They are distinct from anything the
// product writes itself, so a token on a screen is the model's and nobody else's.
const (
	rcHostReply   = "host reply"
	rcLaptopReply = "laptop reply"
	// rcPrimeReply answers the first turn of a chat that goes on to hold a slow
	// one. It is not rcHostReply, because the scenarios that hold a turn look for
	// that word to tell whether the held turn has ended.
	rcPrimeReply = "host primed"
	// rcPartial is what a held turn has already said when a viewer attaches.
	rcPartial = "host partial"
	// rcMark is in every prompt the scenarios type, and it is how the model tells
	// a chat turn of the scenario from a title or a summary the product asks for.
	rcMark = "rc-"
)

// The prompts the scenarios type. `slow` holds the turn at the model; `laptop`
// makes the reply the laptop's. Nothing else in them is read.
const (
	rcSlowAsk   = "rc-host slow question"
	rcHostAsk   = "rc-host quick question"
	rcPrimeAsk  = "rc-host prime question"
	rcLaptopAsk = "rc-laptop typed on the viewer"
)

// How long the contract lets each thing take, with a real clock's margin on the
// ones the contract bounds.
const (
	// rcRowWithin is how long Home may take to list a chat another computer runs.
	rcRowWithin = 60 * time.Second
	// rcOpenWithin is how long a view may take to open after enter.
	rcOpenWithin = 25 * time.Second
	// rcReplyWithin is how long a scripted reply may take to reach a screen.
	rcReplyWithin = 40 * time.Second
	// rcOfflineWithin is the contract's ten seconds for a view to say the host
	// went offline, plus two for the poll and the process kill.
	rcOfflineWithin = 12 * time.Second
	// rcPermitWithin is how long a change to what a computer may do may take to
	// reach a view that is already open.
	rcPermitWithin = 12 * time.Second
)

// ── the whole test ───────────────────────────────────────────────────────────

// TestRemoteControlE2E proves, through the shipped terminal, that a chat another
// computer runs can be opened, worked in and moved from here, with sync storage
// off. Each subtest is one contract line and runs alone.
func TestRemoteControlE2E(t *testing.T) {
	rcRequire(t)
	for _, sub := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"opens_live_and_shows_the_running_turn", rcOpensLiveAndShowsTheRunningTurn},
		{"typing_reaches_the_host_and_both_screens_agree", rcTypingReachesTheHost},
		{"two_viewers_one_keyboard", rcTwoViewersOneKeyboard},
		{"watch_only_cannot_type", rcWatchOnlyCannotType},
		{"host_goes_away_and_comes_back", rcHostGoesAwayAndComesBack},
		{"offline_copy_when_nobody_holds_it", rcOfflineCopyWhenNobodyHoldsIt},
		{"move_here_from_the_live_view", rcMoveHereFromTheLiveView},
		{"removing_a_device_closes_its_view", rcRemovingADeviceClosesItsView},
		{"pairing_default_is_drive_on", rcPairingDefaultIsDriveOn},
		{"old_flags_still_work", rcOldFlagsStillWork},
	} {
		t.Run(sub.name, sub.run)
	}
}

// rcRequire skips the scenario unless it can be run honestly: tmux to drive the
// terminal and the built binary to drive. It asks for no key, because the model
// is a script.
func rcRequire(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}
	binary(t)
}

// ── the scenarios ────────────────────────────────────────────────────────────

// rcOpensLiveAndShowsTheRunningTurn is V1, V2 and V5 (and the class header of
// V21): a turn is running on the host when the viewer opens its chat, the viewer
// sees the words as they are said before the turn ends, and nothing moved.
func rcOpensLiveAndShowsTheRunningTurn(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	host := rc.startHost()
	rc.primeHost(host)
	host.lit(rcSlowAsk)
	host.keys("Enter")
	host.waitFor(30*time.Second, rcPartial)
	rc.abortIfFailed()

	viewer := rc.startViewer(0)
	screen := rc.openLive(viewer)
	if strings.Contains(screen, say(t, "moveAsk")) {
		t.Errorf("enter on a chat another computer is running asked to move it instead of opening it live:\n%s", screen)
	}
	// V2: the turn is still held at the model, so what is on the viewer's screen
	// is a reply that has not ended.
	viewer.waitFor(rcOpenWithin, rcPartial)
	if strings.Contains(viewer.capture(), rcHostReply) {
		t.Errorf("the viewer shows the end of a turn the model is still holding:\n%s", viewer.capture())
	}
	rc.abortIfFailed()

	rc.model.open()
	viewer.waitFor(rcReplyWithin, rcHostReply)
	host.waitFor(rcReplyWithin, rcHostReply, "idle")
	// V1: nothing moved. The host's window is still the chat's own, and Home on
	// the viewer still says the chat is on the host.
	if strings.Contains(host.capture(), "may be behind") {
		t.Errorf("opening the chat live turned the host's own window into a stale copy:\n%s", host.capture())
	}
	rc.goHome(viewer)
	rc.waitForRow(viewer, rcHeldRowWords()...)
	rc.assertLiveDial()
}

// rcTypingReachesTheHost is V3 (and V23 by observation): a line typed on the
// viewer runs on the host, the reply shows on both screens, and neither shows the
// line twice nor does the model get asked twice.
func rcTypingReachesTheHost(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	host := rc.startHost()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	viewer := rc.startViewer(0)
	rc.openLive(viewer)
	// The host typed last, so the viewer is not the one typing yet. Once it is, its
	// empty box says where the words will go.
	rc.takeKeyboard(viewer)
	viewer.waitFor(rcOpenWithin, say(t, "liveMessageHint")+rcHostName)
	rc.abortIfFailed()

	rc.send(viewer, rcLaptopAsk)
	viewer.waitFor(rcReplyWithin, rcLaptopReply)
	host.waitFor(rcReplyWithin, rcLaptopReply)
	rc.abortIfFailed()
	for name, window := range map[string]*rig{"host": host, "viewer": viewer} {
		if n := rcLinesWith(window.capture(), rcLaptopAsk); n != 1 {
			t.Errorf("the %s shows the typed line on %d lines, want exactly one:\n%s", name, n, window.capture())
		}
	}
	if n := rc.model.count(rcLaptopAsk); n != 1 {
		t.Errorf("the model was asked the typed line %d times, want once", n)
	}
	// The line ran where the chat lives and nowhere else: the viewer holds no copy
	// of the conversation, only its own last-seen view.
	if rcTranscriptsWith(t, rc.host.home, rcLaptopAsk) == 0 {
		t.Errorf("the host's own transcript never recorded the line the viewer typed")
	}
	if n := rcTranscriptsWith(t, rc.viewers[0].home, rcLaptopAsk); n != 0 {
		t.Errorf("the viewer wrote %d transcripts holding the line it typed into somebody else's chat", n)
	}
	rc.assertLiveDial()
}

// rcTwoViewersOneKeyboard is V6: with two computers on one chat one holds the
// keyboard, the other says who, and pressing enter there takes it.
func rcTwoViewersOneKeyboard(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 2})
	host := rc.startHost()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	first := rc.startViewer(0)
	rc.openLive(first)
	// The first viewer takes the keyboard by typing a line through.
	rc.send(first, rcLaptopAsk)
	first.waitFor(rcReplyWithin, rcLaptopReply)

	second := rc.startViewer(1)
	rc.openLive(second)
	screen := second.waitFor(rcOpenWithin, say(t, "liveWatching"), rcViewerName)
	if !strings.Contains(screen, say(t, "liveWatching")) {
		t.FailNow()
	}
	// Enter is the documented way to take it, and the one that held it is told.
	second.keys("Enter")
	first.waitFor(rcOpenWithin, say(t, "liveWatching"), rcSecondName)
	const typed = "rc-laptop typed on the second viewer"
	rc.send(second, typed)
	// The first line's reply is already on both screens, so the second one is read
	// as a second reply.
	for name, window := range map[string]*rig{"first viewer": first, "second viewer": second} {
		if !waitUntil(rcReplyWithin, func() bool { return strings.Count(window.capture(), rcLaptopReply) >= 2 }) {
			t.Errorf("the %s never showed the reply to the line typed on the other viewer:\n%s", name, window.capture())
		}
	}
	if n := rc.model.count(typed); n != 1 {
		t.Errorf("the model was asked the second viewer's line %d times, want once", n)
	}
	rc.assertLiveDial()
}

// rcWatchOnlyCannotType is V11: the host takes a computer's typing away while its
// view is open, and within seconds the view says why it cannot type and sends
// nothing. The view draws no box while it says so, so what the scenario can read
// is the sentence and the model's silence.
func rcWatchOnlyCannotType(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	host := rc.startHost()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	viewer := rc.startViewer(0)
	rc.openLive(viewer)
	// While typing is allowed it works, which is what makes the refusal below a
	// change and not a fixture that never could.
	rc.send(viewer, rcLaptopAsk)
	viewer.waitFor(rcReplyWithin, rcLaptopReply)
	rc.abortIfFailed()

	rc.hostCLI("devices", "permit", rcViewerName, "--drive", "off")
	screen := viewer.waitFor(rcPermitWithin, say(t, "liveWatchOnly"))
	if !strings.Contains(screen, say(t, "liveWatchOnly")) {
		t.FailNow()
	}
	const nope = "rc-laptop words that must not be sent"
	viewer.lit(nope)
	viewer.keys("Enter")
	time.Sleep(3 * time.Second)
	if n := rc.model.count(nope); n != 0 {
		t.Errorf("a computer that may only watch got its typing to the model %d times", n)
	}
	if after := viewer.capture(); !strings.Contains(after, say(t, "liveWatchOnly")) {
		t.Errorf("the view stopped saying why it cannot type once somebody tried:\n%s", after)
	}
	if strings.Contains(host.capture(), nope) {
		t.Errorf("the host shows words a watch-only computer typed:\n%s", host.capture())
	}
	rc.assertLiveDial()
}

// rcHostGoesAwayAndComesBack is V7a and V29: the host dies in the middle of a
// turn, the viewer says so within the bound and keeps both the last view and the
// words in its box, and when the host returns the view resumes with the chat
// shown once.
func rcHostGoesAwayAndComesBack(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	host := rc.startHost()
	rc.primeHost(host)
	host.lit(rcSlowAsk)
	host.keys("Enter")
	host.waitFor(30*time.Second, rcPartial)
	viewer := rc.startViewer(0)
	rc.openLive(viewer)
	viewer.waitFor(rcOpenWithin, rcPartial)
	// The host has the keyboard, and a box that is not on the frame takes no
	// characters, so the viewer takes it before it types the words it will keep.
	rc.takeKeyboard(viewer)
	const draft = "rc-laptop draft kept across the drop"
	viewer.lit(draft)
	rc.abortIfFailed()

	host.loseLid()
	screen := viewer.waitFor(rcOfflineWithin, say(t, "liveWentOffline"))
	if !strings.Contains(screen, rcPartial) {
		t.Errorf("the view lost what it showed when the host went away:\n%s", screen)
	}
	rc.abortIfFailed()

	// The host comes back the way a person brings it back, by starting codeaf in
	// the same folder, and the gate is open so a turn it takes up again ends.
	rc.model.open()
	rc.startHost()
	if !waitUntil(90*time.Second, func() bool {
		s := viewer.capture()
		return strings.Contains(s, say(t, "liveLegend")) && !strings.Contains(s, say(t, "liveWentOffline"))
	}) {
		t.Fatalf("the view never resumed after the host came back:\n%s", viewer.capture())
	}
	after := viewer.capture()
	if n := rcLinesWith(after, rcSlowAsk); n != 1 {
		t.Errorf("after the resume the question is on %d lines, want one:\n%s", n, after)
	}
	// The box is the viewer's again once it has the keyboard, and the words it held
	// through the drop are in it.
	rc.takeKeyboard(viewer)
	if after = viewer.capture(); !strings.Contains(after, draft) {
		t.Errorf("the words in the box did not survive the resume:\n%s", after)
	}
	// The resumed view is a working one, and the line sent from it is asked once.
	viewer.keys("Enter")
	viewer.waitFor(rcReplyWithin, rcLaptopReply)
	if n := rc.model.count(draft); n != 1 {
		t.Errorf("the draft was asked of the model %d times, want once", n)
	}
	rc.assertLiveDial()
}

// rcOfflineCopyWhenNobodyHoldsIt is V27: with the host gone for good and storage
// off, the viewer opens the same chat from its own last-seen copy, read-only with
// a dated banner, and what it types goes nowhere.
func rcOfflineCopyWhenNobodyHoldsIt(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	host := rc.startHost()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	viewer := rc.startViewer(0)
	rc.openLive(viewer)
	// The copy is made from what the view showed, so the viewer waits until it
	// has shown the reply and a moment longer for the copy to reach its disk.
	viewer.waitFor(rcOpenWithin, rcHostReply)
	time.Sleep(3 * time.Second)
	rc.goHome(viewer)

	host.loseLid()
	rc.waitForRow(viewer, rcHostName+" offline")
	rc.openRow(viewer, rcHostName+" offline")
	screen := viewer.waitFor(rcOpenWithin, "last seen", rcHostReply)
	if !rcSeenAt.MatchString(screen) {
		t.Errorf("the banner of a copy does not carry the time it was last seen:\n%s", screen)
	}
	rc.abortIfFailed()

	// Whatever is typed into a copy goes nowhere: the host is not there to take it,
	// and no computer starts a chat on another's behalf.
	const words = "rc-laptop words typed into a copy"
	viewer.lit(words)
	viewer.keys("Enter")
	time.Sleep(3 * time.Second)
	after := viewer.capture()
	if strings.Contains(after, rcLaptopReply) || rc.model.count(words) != 0 {
		t.Errorf("a read-only copy of a chat nobody runs sent what was typed into it:\n%s", after)
	}
	if !strings.Contains(after, "last seen") {
		t.Errorf("the copy stopped saying it is a copy once somebody tried to type into it:\n%s", after)
	}
}

// rcMoveHereFromTheLiveView is V8 and V9: alt+m in a live view asks, a move that
// cannot reach the host leaves the viewer in the live view with nothing lost, and
// a move that can shows Moving here and then runs the chat on the viewer.
func rcMoveHereFromTheLiveView(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	host := rc.startHost()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	viewer := rc.startViewer(0)
	rc.openLive(viewer)

	// V9, first: the host's door stops answering in the middle of the move. A chat
	// nobody is working in can be taken without asking its computer anything, so the
	// move is tried on a chat with a turn running: the host holds it and has to let
	// it go. The card is answered while the host is stopped, so the move cannot
	// finish.
	host.lit(rcSlowAsk)
	host.keys("Enter")
	host.waitFor(30*time.Second, rcPartial)
	viewer.waitFor(rcOpenWithin, rcPartial)
	rc.abortIfFailed()
	viewer.keys("M-m")
	viewer.waitFor(rcOpenWithin, say(t, "moveAsk"))
	rc.abortIfFailed()
	resume := rc.pauseHost()
	viewer.keys("1", "Enter")
	// The move starts before it fails, so the viewer is first given a moment to
	// say it is on its way; only then is "back in the live view" a change.
	waitUntilFast(10*time.Second, func() bool { return strings.Contains(viewer.capture(), say(t, "movingHere")) })
	if !waitUntil(100*time.Second, func() bool {
		s := viewer.capture()
		return !strings.Contains(s, say(t, "movingHere")) && !strings.Contains(s, say(t, "moveAsk")) &&
			(strings.Contains(s, say(t, "liveMessageHint")+rcHostName) || strings.Contains(s, say(t, "liveWentOffline")))
	}) {
		t.Fatalf("a move that could not reach the host did not put the viewer back in the live view:\n%s", viewer.capture())
	}
	if strings.Contains(viewer.capture(), "Moved from") {
		t.Errorf("a move the host never answered claims the chat arrived:\n%s", viewer.capture())
	}
	resume()
	if !waitUntil(60*time.Second, func() bool {
		s := viewer.capture()
		return strings.Contains(s, say(t, "liveLegend")) && !strings.Contains(s, say(t, "liveWentOffline"))
	}) {
		t.Fatalf("the live view did not come back once the host answered again:\n%s", viewer.capture())
	}

	// The held turn ends, so the chat is one nobody is working in again.
	rc.model.open()
	if !waitUntil(rcReplyWithin, func() bool {
		s := host.capture()
		return strings.Contains(s, "idle") && rcLinesWith(s, rcHostReply) >= 2
	}) {
		t.Fatalf("the held turn never ended on the host:\n%s", host.capture())
	}

	// V8: the same move with the host answering. The sentence is short-lived, so
	// the screen is read quickly from the answer on, and the arrival is what the
	// scenario insists on.
	viewer.keys("M-m")
	viewer.waitFor(rcOpenWithin, say(t, "moveAsk"))
	viewer.keys("1", "Enter")
	sawMoving := false
	if !waitUntilFast(rcReplyWithin+30*time.Second, func() bool {
		s := viewer.capture()
		sawMoving = sawMoving || strings.Contains(s, say(t, "movingHere"))
		return strings.Contains(s, "Moved from "+rcHostName)
	}) {
		t.Fatalf("the chat never arrived on the viewer (moving seen: %v):\n%s", sawMoving, viewer.capture())
	}
	if !sawMoving {
		t.Errorf("the viewer never said %q while the chat was on its way", say(t, "movingHere"))
	}
	rc.send(viewer, rcLaptopAsk)
	viewer.waitFor(rcReplyWithin, rcLaptopReply, "idle")
	if strings.Contains(viewer.capture(), say(t, "liveLegend")) {
		t.Errorf("the chat runs on the viewer now, and its window still calls itself a live view:\n%s", viewer.capture())
	}
	// The host's own window of the chat becomes a live view of the new holder.
	host.waitFor(rcReplyWithin, say(t, "liveLegend"))
}

// rcRemovingADeviceClosesItsView is V10: taking a computer off the person's list
// on the host closes that computer's open view within the bound, and Home there
// stops listing the host's chats.
func rcRemovingADeviceClosesItsView(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	host := rc.startHost()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	viewer := rc.startViewer(0)
	rc.openLive(viewer)

	rc.hostCLI("devices", "revoke", rcViewerName)
	if !waitUntil(rcPermitWithin, func() bool { return !strings.Contains(viewer.capture(), say(t, "liveLegend")) }) {
		t.Fatalf("the removed computer's view stayed open:\n%s", viewer.capture())
	}
	if !waitUntil(30*time.Second, func() bool {
		s := viewer.capture()
		for _, words := range append(rcHeldRowWords(), rcHostName+" offline") {
			if strings.Contains(s, words) {
				return false
			}
		}
		return true
	}) {
		t.Errorf("Home on the removed computer still lists the host's chats:\n%s", viewer.capture())
	}
}

// rcPairingDefaultIsDriveOn is V25 for the code path: two fresh homes pair with
// `codeaf pair --code`, the approving screen says the new computer can also see
// and control the chats, and the new computer can type into one with no further
// step.
func rcPairingDefaultIsDriveOn(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1, unpaired: true})
	if _, err := devname.Set(rc.host.home, rcHostName); err != nil {
		t.Fatal(err)
	}
	offer := startCommand(t, rc.env(), "rc-offer", rc.host.home, rc.host.work, "pair", "--code")
	var code string
	if !waitUntil(30*time.Second, func() bool {
		code = rcCodeShape.FindString(offer.capture())
		return code != ""
	}) {
		t.Fatalf("pair --code never showed a code:\n%s", offer.capture())
	}
	join := startCommand(t, rc.env(), "rc-join", rc.viewers[0].home, rc.viewers[0].work, "pair", "--name", rcViewerName, code)
	offer.waitFor(30*time.Second, "wants your chats")
	rc.abortIfFailed()
	offer.lit("y")
	offer.keys("Enter")
	screen := offer.waitFor(30*time.Second, say(t, "pairedControlLine"))
	if !strings.Contains(screen, say(t, "pairedControlLine")) {
		t.FailNow()
	}
	join.waitFor(30*time.Second, "Paired")
	rc.abortIfFailed()
	rc.settlePairing()

	host := rc.startHost()
	rc.hostAsk(host, rcHostAsk, rcHostReply)
	viewer := rc.startViewer(0)
	rc.openLive(viewer)
	// No toggle, no card, no key beyond the one that takes the keyboard.
	rc.send(viewer, rcLaptopAsk)
	viewer.waitFor(rcReplyWithin, rcLaptopReply)
	host.waitFor(rcReplyWithin, rcLaptopReply)
	rc.assertLiveDial()
}

// rcOldFlagsStillWork is V14: with remote control on, `codeaf serve` and
// `codeaf chat --at` behave as they always did, and nothing about a live view
// shows on either screen.
func rcOldFlagsStillWork(t *testing.T) {
	rc := newRCRig(t, rcOptions{viewers: 1})
	serve := rcStartPlain(t, rc.env(), "rc-serve", rc.host, 120, 40, "serve", "--via", rc.address)
	var name, code string
	if !waitUntil(30*time.Second, func() bool {
		s := serve.capture()
		if m := rcReachName.FindStringSubmatch(s); m != nil {
			name = m[1]
		}
		if m := rcServeCode.FindStringSubmatch(s); m != nil {
			code = m[1]
		}
		return name != "" && code != ""
	}) {
		t.Fatalf("codeaf serve never showed its name and code:\n%s", serve.capture())
	}
	at := rcStartPlain(t, rc.env(), "rc-at", rc.viewers[0], 180, 55, "chat", "--at", name)
	at.waitFor(30*time.Second, "enter the code shown on")
	at.lit(code)
	at.keys("Enter")
	serve.waitFor(30*time.Second, "wants to use this machine")
	serve.lit("y")
	serve.keys("Enter")
	if hit, _ := at.waitForAny(60*time.Second, say(t, "placeRestWord"), say(t, "landingKeysWord"),
		say(t, "welcomeStarterKeysWord"), say(t, "starterTaskWord")); hit == "" {
		t.FailNow()
	}
	at.lit(rcLaptopAsk)
	at.keys("Enter")
	at.waitFor(rcReplyWithin, rcLaptopReply)
	for _, window := range []*rig{serve, at} {
		screen := window.capture()
		for _, word := range []string{say(t, "liveLegend"), say(t, "liveWentOffline"), say(t, "liveWatchOnly"), say(t, "liveWatching")} {
			if strings.Contains(screen, word) {
				t.Errorf("an older door shows %q, a word of live control:\n%s", word, screen)
			}
		}
	}
}

// ── the rig ──────────────────────────────────────────────────────────────────

// rcOptions says what a scenario's computers start as.
type rcOptions struct {
	// viewers is how many viewing computers there are, one or two.
	viewers int
	// unpaired leaves every home with no identity, for the scenario that pairs
	// them through the product's own pairing and reads what that grants.
	unpaired bool
}

// rcHome is one computer of the rig: its state root, the project it works in, and
// its device id once it is paired.
type rcHome struct {
	name, home, work, device string
}

// rcRig is one scenario's world: a short root under /tmp, the scripted model, the
// auditing relay, and the paired homes. It is built by [newRCRig] and owned by
// the one subtest that built it.
type rcRig struct {
	t        *testing.T
	root     string
	model    *rcModel
	audit    *rcAudit
	address  string
	identity string
	host     rcHome
	viewers  []rcHome
	windows  int
}

// newRCRig builds a scenario's world and leaves every window to the scenario.
//
// THE ROOT IS SHORT AND UNDER /tmp. A unix socket's path may not pass 103 bytes
// and the engine's lives under the state root, so a root under a long temporary
// folder would make every chat fall back to the in-process road this scenario is
// not about.
func newRCRig(t *testing.T, opt rcOptions) *rcRig {
	t.Helper()
	rcRequire(t)
	if opt.viewers < 1 {
		opt.viewers = 1
	}
	root, err := os.MkdirTemp("/tmp", "rc-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	rc := &rcRig{t: t, root: root, model: startRCModel(t)}
	rc.address, rc.audit = startAuditedRelay(t, root)
	t.Setenv(syncsetup.URLVar, rc.address)

	names := []string{rcViewerName, rcSecondName}
	rc.host = rcHome{name: rcHostName, home: filepath.Join(root, "h"), work: filepath.Join(root, "hw")}
	for i := 0; i < opt.viewers; i++ {
		suffix := fmt.Sprint(i)
		rc.viewers = append(rc.viewers, rcHome{name: names[i], home: filepath.Join(root, "v"+suffix), work: filepath.Join(root, "vw"+suffix)})
	}
	for _, h := range rc.homes() {
		if err := os.MkdirAll(h.home, 0o700); err != nil {
			t.Fatal(err)
		}
		workspaceAt(t, h.work, false)
		rc.writeConfig(h.home)
	}
	// Engines belong to these temporary homes, including any a move brings along.
	t.Cleanup(func() {
		pids := engineDaemonsUnder(root)
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		deadline := time.Now().Add(10 * time.Second)
		for _, pid := range pids {
			for !terminalProcessExited(pid) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if !terminalProcessExited(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	if opt.unpaired {
		// The product's own pairing makes the identity, the roster and the
		// storage answer, so nothing is written here, and the audit stays quiet
		// until the scenario has settled them.
		rc.audit.armed.Store(false)
		return rc
	}
	rc.pairDirectly()
	return rc
}

// homes is every computer of the rig, the host first.
func (rc *rcRig) homes() []rcHome { return append([]rcHome{rc.host}, rc.viewers...) }

// writeConfig gives a home the rows every scenario here wants: a stub key, the
// scripted model in every tier, no approval prompts, the plain marks and a setup
// that has been seen.
func (rc *rcRig) writeConfig(home string) {
	rc.t.Helper()
	rows := map[string]any{"api_key": "stub-key", "model.talk": automationStubModel, "memory.enabled": false, "tools.approvalMode": "allow", "setup_seen_at": time.Now().UTC().Format(time.RFC3339), config.KeyIcons: config.IconsPlain}
	for _, key := range []string{config.KeyTaskModel, config.KeyTierWorkerModel, config.KeyTierLowModel, config.KeyTierHighModel, config.KeyTierReflexModel, config.KeyTierMastermindModel} {
		rows[key] = automationStubModel
	}
	raw, _ := json.Marshal(rows)
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
		rc.t.Fatal(err)
	}
}

// pairDirectly pairs the homes without driving the pairing screens: one identity,
// every other home adopting it with a key of its own, each computer named and
// registered in the directory, and sync storage off on all of them. Then it
// writes the host's explicit permit for each viewer, because a computer on the
// roster before the host first reads its book would be an older pairing, and an
// older pairing cannot type.
func (rc *rcRig) pairDirectly() {
	t := rc.t
	t.Helper()
	id, err := identity.Ensure(rc.host.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range rc.viewers {
		if _, err := identity.Adopt(v.home, id, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range rc.homes() {
		if _, err := devname.Set(h.home, h.name); err != nil {
			t.Fatal(err)
		}
		s, ok, err := syncsetup.Open(h.home)
		if err != nil || !ok {
			t.Fatalf("paired home %s: %v %v", h.name, ok, err)
		}
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, h.name); err != nil {
			t.Fatal(err)
		}
		if err := syncsetup.SaveStorage(h.home, rc.address, s.Identity.ID(), "off"); err != nil {
			t.Fatal(err)
		}
		rc.identity = s.Identity.ID()
		switch {
		case h.home == rc.host.home:
			rc.host.device = s.Device.ID()
		default:
			for i := range rc.viewers {
				if rc.viewers[i].home == h.home {
					rc.viewers[i].device = s.Device.ID()
				}
			}
		}
	}
	for _, h := range rc.homes() {
		rcStampJoin(t, h.home, rc.identity)
	}
	for _, v := range rc.viewers {
		rcGrant(t, rc.host.home, rc.identity, v.device, true, true)
	}
}

// settlePairing is what a scenario that paired through the product does before
// its first window: it reads the identity the pairing made, turns sync storage
// off on every home (a self-hosted relay answers on, and the point here is the
// live road alone), and only then starts holding the relay to its no-store law.
func (rc *rcRig) settlePairing() {
	t := rc.t
	t.Helper()
	id, err := identity.Load(rc.host.home)
	if err != nil {
		t.Fatalf("the pairing left the host with no identity: %v", err)
	}
	rc.identity = id.ID()
	for _, h := range rc.homes() {
		if err := syncsetup.SaveStorage(h.home, rc.address, rc.identity, "off"); err != nil {
			t.Fatal(err)
		}
	}
	rc.audit.armed.Store(true)
}

// env is what every window and command of the rig runs with. The key is a string
// and not a secret: nothing behind the model asks for one.
func (rc *rcRig) env() []string {
	return []string{config.APIKeyEnv + "=stub-key", "CODEAF_BASE_URL=" + rc.model.server.URL + "/api/v1", "CODEAF_PROFILE_DIR=",
		syncsetup.URLVar + "=" + rc.address, "CODEAF_SYNC_INTERVAL_MS=100", "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1",
		"CODEAF_NO_UPDATE_CHECK=1", "CODEAF_NO_AUTOMATIONS=1", "CODEAF_REMOTE_CONTROL=1"}
}

// window opens one terminal on a computer. Names are numbered so a window that
// comes back (the host after it was lost) never collides with the one it replaces.
func (rc *rcRig) window(name string, h rcHome) *rig {
	rc.t.Helper()
	rc.windows++
	return startWithEnv(rc.t, rc.env(), fmt.Sprintf("rc-%s-%d", name, rc.windows), h.home, h.work, 180, 55)
}

func (rc *rcRig) startHost() *rig { return rc.window("host", rc.host) }

func (rc *rcRig) startViewer(i int) *rig { return rc.window("viewer", rc.viewers[i]) }

// abortIfFailed ends a scenario once something it stands on has failed, so a
// missing view does not cost a wait for every step behind it.
func (rc *rcRig) abortIfFailed() {
	rc.t.Helper()
	if rc.t.Failed() {
		rc.t.FailNow()
	}
}

// hostKnowsItsOthers waits until the host's window has read the roster and written
// down that another computer is paired. A chat that finishes a turn before that is
// kept on the host for want of a second computer to share it with, and is on no
// other computer's Home until a later turn ends, which is a fact about how soon
// after starting a person asks and not about anything this suite checks. The wait
// is soft: a host that never learns it goes on, and the scenario fails where it
// needed the chat.
func (rc *rcRig) hostKnowsItsOthers() {
	rc.t.Helper()
	waitUntil(30*time.Second, func() bool {
		found, _ := filepath.Glob(filepath.Join(rc.host.home, "v3", "sync", "others", "*"))
		return len(found) > 0
	})
}

// hostAsk has the host's own window ask one question and waits for the reply, and
// then for the chat to be on the relay's directory. The second wait is what a
// person does without noticing: the desktop's turn ends, and a minute later the
// laptop is opened. Starting the viewer the moment the reply shows would put its one
// look at the directory before the desktop had written the chat there, and Home
// asks again only on a slow beat, so a scenario would then wait out the beat.
func (rc *rcRig) hostAsk(w *rig, words, reply string) {
	rc.t.Helper()
	rc.hostKnowsItsOthers()
	before := rc.audit.cellCreates()
	w.lit(words)
	w.keys("Enter")
	w.waitFor(rcReplyWithin, reply, "idle")
	rc.abortIfFailed()
	if before == 0 {
		waitUntil(150*time.Second, func() bool { return rc.audit.cellCreates() > 0 })
	}
}

// primeHost has the host's window finish one quick turn before the scenario
// holds a slow one. A chat is on the other computers' Home only once its first
// turn has been written down, so a chat whose very first turn is still running
// is not there to open yet: the scenarios that hold a turn at the model hold the
// second turn of a chat, which is also the commoner thing for a person to watch.
func (rc *rcRig) primeHost(w *rig) {
	rc.t.Helper()
	rc.hostAsk(w, rcPrimeAsk, rcPrimeReply)
}

// goHome opens Home from wherever the window is, with the chord that does it in
// every state of a conversation, a live view included.
func (rc *rcRig) goHome(r *rig) {
	rc.t.Helper()
	r.keys("M-1")
	r.waitFor(15*time.Second, say(rc.t, "placeRestWord"))
	rc.abortIfFailed()
}

// rcHeldRowWords are the two ways Home says a chat another computer holds is
// there to open: a turn running, or the chat held with nothing running.
func rcHeldRowWords() []string {
	return []string{"working on " + rcHostName, "held on " + rcHostName}
}

// waitForRow waits until Home shows a line saying one of the words.
func (rc *rcRig) waitForRow(r *rig, words ...string) {
	rc.t.Helper()
	if hit, _ := r.waitForAny(rcRowWithin, words...); hit == "" {
		rc.t.FailNow()
	}
}

// openRow points at the Home row that says one of the words, and presses enter on
// it. The pointer is moved onto the row and not clicked, because what is under
// test is enter, which acts on the row the pointer last selected.
func (rc *rcRig) openRow(r *rig, words ...string) {
	rc.t.Helper()
	deadline := time.Now().Add(rcRowWithin)
	for time.Now().Before(deadline) {
		for y, line := range strings.Split(r.capture(), "\n") {
			for _, word := range words {
				if at := strings.Index(line, word); at >= 0 {
					r.mouseTo(ansi.StringWidth(line[:at])+2, y+1)
					r.keys("Enter")
					return
				}
			}
		}
		time.Sleep(pollEvery)
	}
	rc.t.Fatalf("Home never listed a row saying any of %q. The screen was:\n%s", words, r.capture())
}

// openLive opens, from a viewer's Home, the chat the host holds, and answers the
// screen it opened on. It fails the scenario when the view does not say it is
// live, which is the one thing every scenario here stands on.
func (rc *rcRig) openLive(v *rig) string {
	rc.t.Helper()
	rc.goHome(v)
	rc.openRow(v, rcHeldRowWords()...)
	screen := v.waitFor(rcOpenWithin, say(rc.t, "liveLegend"), rcHostName)
	rc.abortIfFailed()
	return screen
}

// takeKeyboard makes this window the one that types, the way the view says to:
// when it reads `typing from <name> now` another computer has the keyboard, no
// character typed reaches a box that is not on the frame, and enter takes the
// keyboard and brings the box back with whatever was in it. A window that
// already has the keyboard is left alone.
func (rc *rcRig) takeKeyboard(r *rig) {
	rc.t.Helper()
	if !strings.Contains(r.capture(), say(rc.t, "liveWatching")) {
		return
	}
	r.keys("Enter")
	if !waitUntil(10*time.Second, func() bool { return !strings.Contains(r.capture(), say(rc.t, "liveWatching")) }) {
		rc.t.Errorf("enter did not take the keyboard:\n%s", r.capture())
	}
}

// send types words into a window and sends them.
func (rc *rcRig) send(r *rig, words string) {
	rc.t.Helper()
	rc.takeKeyboard(r)
	r.lit(words)
	r.keys("Enter")
}

// submit sends what is already in the box.
func (rc *rcRig) submit(r *rig) {
	rc.t.Helper()
	rc.takeKeyboard(r)
	r.keys("Enter")
}

// hostCLI runs one of the product's own commands on the host, as a person would
// from a terminal on that computer, and answers what it printed.
func (rc *rcRig) hostCLI(args ...string) string {
	rc.t.Helper()
	ctx, cancel := context.WithTimeout(rc.t.Context(), time.Minute)
	defer cancel()
	env := append(os.Environ(), rc.env()...)
	env = append(env, "CODEAF_HOME="+rc.host.home, "TERM=xterm-256color")
	cmd := guardedCommand(rc.t, ctx, rc.host.home, env, binary(rc.t), args...)
	cmd.Dir = rc.host.work
	out, err := cmd.CombinedOutput()
	if err != nil {
		rc.t.Fatalf("codeaf %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// pauseHost stops every process of the host's home without ending any of them,
// which is how a computer that stops answering in the middle of a move looks from
// the other side. It is the window and the engine both, because the door a move
// goes through is the window's and the door a live view goes through is the
// engine's, and stopping one alone leaves the other answering. The answer resumes
// them, and a cleanup does too, so a failed scenario never leaves a frozen process
// for the teardown to wait on.
func (rc *rcRig) pauseHost() func() {
	rc.t.Helper()
	pids := processesOfHome(rc.host.home)
	if len(pids) == 0 {
		rc.t.Fatalf("the host has no process of its own to stop")
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGSTOP)
	}
	var once sync.Once
	resume := func() {
		once.Do(func() {
			for _, pid := range pids {
				_ = syscall.Kill(pid, syscall.SIGCONT)
			}
		})
	}
	rc.t.Cleanup(resume)
	return resume
}

// processesOfHome is every process that runs with this home as its CODEAF_HOME,
// read from the process table by the environment each was started with and never
// by a pattern in a command line.
func processesOfHome(home string) []int {
	want := "CODEAF_HOME=" + home + "\x00"
	entries, _ := os.ReadDir("/proc")
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err == nil && strings.Contains(string(env), want) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// assertLiveDial holds the relay to the one thing every scenario that opens a
// view must show it: at least one dial that said it was a live one.
func (rc *rcRig) assertLiveDial() {
	rc.t.Helper()
	if n := rc.audit.dials(relay.DoorLive); n == 0 {
		rc.t.Errorf("the relay saw no dial that said %s: %s", relay.HeaderDoor+": "+relay.DoorLive, rc.audit.summary())
	}
}

// rcStartPlain opens a command that asks a question on a plain terminal before it
// draws anything, which is how `serve` and the first `chat --at` begin. The
// package's door waits for a surface the command has not drawn yet, so the verb is
// marked headless for the one launch and put back.
func rcStartPlain(t *testing.T, env []string, name string, h rcHome, cols, rows int, args ...string) *rig {
	t.Helper()
	verb := firstWord(args)
	was, had := headlessVerbs[verb]
	headlessVerbs[verb] = true
	defer func() {
		if had {
			headlessVerbs[verb] = was
		} else {
			delete(headlessVerbs, verb)
		}
	}()
	return startWithEnv(t, env, name, h.home, h.work, cols, rows, args...)
}

// ── the scripted model ───────────────────────────────────────────────────────

// rcModel is the scripted model: an endpoint that answers a chat turn by what the
// typed words say, and holds a turn whose words say `slow` until the gate opens.
type rcModel struct {
	server *httptest.Server
	gate   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	asked  []string
}

func startRCModel(t *testing.T) *rcModel {
	t.Helper()
	m := &rcModel{gate: make(chan struct{})}
	brain := &automationBrain{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", brain.serveCatalog)
	mux.HandleFunc("/api/v1/chat/completions", m.complete)
	m.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		m.open()
		m.server.Close()
	})
	return m
}

// open lets every held turn finish. It is idempotent.
func (m *rcModel) open() { m.once.Do(func() { close(m.gate) }) }

// count is how many chat turns asked the model about words.
func (m *rcModel) count(words string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, ask := range m.asked {
		if strings.Contains(ask, words) {
			n++
		}
	}
	return n
}

// rcMessageText is the text of one chat message, whether the endpoint was handed
// a plain string or a list of parts.
func rcMessageText(raw json.RawMessage) string {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var all []string
	for _, part := range parts {
		all = append(all, part.Text)
	}
	return strings.Join(all, "\n")
}

func (m *rcModel) complete(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(raw, &body)
	// The question being answered is the newest message that carries the marker
	// every typed prompt has, because a conversation's history is sent each time.
	ask := ""
	for i := len(body.Messages) - 1; i >= 0 && ask == ""; i-- {
		if !strings.EqualFold(body.Messages[i].Role, "user") {
			continue
		}
		if text := rcMessageText(body.Messages[i].Content); strings.Contains(text, rcMark) {
			ask = text
		}
	}
	send, ok := automationStream(w)
	if !ok {
		return
	}
	// A request that offers no tools is the product naming or summarising the
	// chat. It is answered with one word that no scenario waits for.
	if ask == "" || len(body.Tools) == 0 {
		send(`{"content":"ok"}`, "")
		send(`{}`, "stop")
		send("", "[DONE]")
		return
	}
	m.mu.Lock()
	m.asked = append(m.asked, ask)
	m.mu.Unlock()
	if strings.Contains(ask, "slow") {
		send(fmt.Sprintf(`{"content":%q}`, rcPartial), "")
		// A held answer still keeps the line alive: a stream that says nothing for
		// ten seconds is read as a dead path and asked again, so the hold writes
		// the comment a busy router writes, once a second.
		beat := time.NewTicker(time.Second)
		defer beat.Stop()
		stop := time.After(2 * time.Minute)
	held:
		for {
			select {
			case <-m.gate:
				break held
			case <-stop:
				break held
			case <-r.Context().Done():
				return
			case <-beat.C:
				keepalive(w)
			}
		}
		send(fmt.Sprintf(`{"content":%q}`, " "+rcHostReply), "")
		send(`{}`, "stop")
		send("", "[DONE]")
		return
	}
	reply := rcHostReply
	switch {
	case strings.Contains(ask, "laptop"):
		reply = rcLaptopReply
	case strings.Contains(ask, "prime"):
		reply = rcPrimeReply
	}
	send(fmt.Sprintf(`{"content":%q}`, reply), "")
	send(`{}`, "stop")
	send("", "[DONE]")
}

// ── the auditing relay ───────────────────────────────────────────────────────

// rcCall is one request the proxy saw.
type rcCall struct{ method, path, door string }

// rcAudit writes down every request that passes through the relay's proxy and
// holds the relay to the law of this suite: with sync storage off, no one asks it
// to store anything.
type rcAudit struct {
	mu          sync.Mutex
	calls       []rcCall
	armed       atomic.Bool
	storeWrites atomic.Int32
}

// startAuditedRelay starts the product's own relay and a proxy in front of it.
// The proxy answers a store write the way the hosted service answers it while
// storage is off, and the scenario fails if one arrived.
func startAuditedRelay(t *testing.T, root string) (string, *rcAudit) {
	t.Helper()
	upstream, err := url.Parse(quitContinuationRelay(t, root))
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	audit := &rcAudit{}
	audit.armed.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		audit.mu.Lock()
		audit.calls = append(audit.calls, rcCall{method: r.Method, path: r.URL.Path, door: r.Header.Get(relay.HeaderDoor)})
		audit.mu.Unlock()
		// The hosted service says, on every reading of the account's storage, that
		// it is off. A self-hosted relay would answer on, and a computer that
		// believes that tries an upload, is refused, and tries again, which is a
		// property of the fixture and not of the live road.
		if r.Method == http.MethodGet && r.URL.Path == "/v1/store/stats" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"storage":"off","puts":0,"gets":0,"has":0,"bytes_in":0,"bytes_out":0}`))
			return
		}
		// An upload is the only request that stores something: a Has or a Locate
		// asks what the relay holds and writes nothing.
		if r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" {
			if audit.armed.Load() {
				audit.storeWrites.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"storage_off"}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		if n := audit.storeWrites.Load(); n > 0 {
			t.Errorf("the relay was asked to store something %d times with sync storage off: %s", n, audit.summary())
		}
	})
	return server.URL, audit
}

// cellCreates is how many chats have been written to the relay's directory so far:
// each is one POST to a cell's own path and nothing after it.
func (a *rcAudit) cellCreates() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, call := range a.calls {
		rest, ok := strings.CutPrefix(call.path, "/v1/dir/cells/")
		if call.method == http.MethodPost && ok && rest != "" && !strings.Contains(rest, "/") {
			n++
		}
	}
	return n
}

// dials counts the requests that claimed a class of dial.
func (a *rcAudit) dials(class string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, call := range a.calls {
		if call.door == class {
			n++
		}
	}
	return n
}

// summary is the paths the proxy saw, counted, for a failure to quote.
func (a *rcAudit) summary() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	counts := map[string]int{}
	for _, call := range a.calls {
		key := call.method + " " + call.path
		if call.door != "" {
			key += " [" + call.door + "]"
		}
		counts[key]++
	}
	var lines []string
	for key, n := range counts {
		lines = append(lines, fmt.Sprintf("%s x%d", key, n))
	}
	return strings.Join(lines, "; ")
}

// ── reading a screen ─────────────────────────────────────────────────────────

var (
	// rcCodeShape is a pairing code as the two doors show it, nameplate and six
	// digits.
	rcCodeShape = regexp.MustCompile(`\b\d{1,4}-\d{3}-\d{3}\b`)
	// rcServeCode is the code `codeaf serve` shows, as two groups of three digits.
	rcServeCode = regexp.MustCompile(`with code\s+(\d{3}\s\d{3})`)
	// rcReachName is the name `codeaf serve` says the machine is reachable as.
	rcReachName = regexp.MustCompile(`reachable as\s+(\S+)`)
	// rcSeenAt is the time on the banner of a copy nobody is running.
	rcSeenAt = regexp.MustCompile(`last seen (?:\d{1,2} \w{3} )?\d{1,2}:\d{2}`)
)

// rcLinesWith is how many lines of a screen hold the words.
func rcLinesWith(screen, words string) int {
	n := 0
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, words) {
			n++
		}
	}
	return n
}

// rcTranscriptsWith is how many conversations a home wrote that hold the words.
func rcTranscriptsWith(t *testing.T, home, words string) int {
	t.Helper()
	n := 0
	for _, text := range sessionTranscripts(t, home) {
		if strings.Contains(text, words) {
			n++
		}
	}
	return n
}

// waitUntilFast is [waitUntil] for a state that is on the screen for a moment: it
// reads the screen every fifty milliseconds instead of every quarter second.
func waitUntilFast(within time.Duration, fact func() bool) bool {
	deadline := time.Now().Add(within)
	for {
		if fact() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
