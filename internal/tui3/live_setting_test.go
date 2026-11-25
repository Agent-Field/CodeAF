package tui3

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// THE SETTING IS READ LIVE. Remote control is turned on and off in /settings
// while a window is open, so whether Enter on a chat another computer holds opens
// it live or asks to move it must follow the setting at the moment of the Enter,
// not the setting at the moment the window started. These tests state that from
// the person's side: the setting is a function the test flips, the rest is the
// Home screen as the other live-view tests drive it.

// liveSetting is the setting as a test switches it.
type liveSetting struct{ on bool }

func (s *liveSetting) read() bool { return s.on }

// settingRig is a Home with one chat held by an online computer of the
// person's own, the live opener wired, and the setting read through a function
// the returned value flips.
func settingRig(t *testing.T, on bool) (*app, *fakeTaker, *liveSetting, *[]chatlist.Row) {
	t.Helper()
	a, taker := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, nil)
	setting := &liveSetting{on: on}
	a.remoteControl = setting.read
	return a, taker, setting, calls
}

// C1: a window that began with remote control off, then had it switched on,
// opens a held chat live on the next Enter, and never raised the Move card for it.
func TestAWindowSwitchedOnAfterItStartedOpensAHeldChatLive(t *testing.T) {
	a, taker, setting, calls := settingRig(t, false)

	standOn(t, a, "c1")
	if !moveCardRaised(a) || len(*calls) != 0 || !a.at(pageHome) {
		t.Fatalf("with remote control off the Enter did not end in the Move card: card=%v opens=%d home=%v", moveCardRaised(a), len(*calls), a.at(pageHome))
	}

	setting.on = true
	standOn(t, a, "c1")
	if len(*calls) != 1 || (*calls)[0].Cell != "c1" {
		t.Fatalf("after switching remote control on the Enter made %d live opens, want one for c1", len(*calls))
	}
	if a.liveFront() == nil || a.liveFront().Cell != "c1" || a.at(pageHome) {
		t.Fatalf("after switching remote control on the Enter did not end on a live view: live=%v home=%v", a.liveFront(), a.at(pageHome))
	}
	if len(taker.cells) != 0 {
		t.Fatalf("the live open moved the chat: %v", taker.cells)
	}
}

// C1, the other direction: switching it off in a window that has the opener
// brings the Move card back at once.
func TestAWindowSwitchedOffAfterItStartedAsksToMoveAHeldChat(t *testing.T) {
	a, _, setting, calls := settingRig(t, true)
	setting.on = false

	standOn(t, a, "c1")
	if !moveCardRaised(a) || len(*calls) != 0 || !a.at(pageHome) {
		t.Fatalf("with remote control switched off the Enter did not end in the Move card: card=%v opens=%d home=%v", moveCardRaised(a), len(*calls), a.at(pageHome))
	}
}

// C2: with remote control off, a window that has the opener wired behaves BYTE
// FOR BYTE as a window that has none: the same frame, the same foot, no live
// attempt, and no wait for the roster that a held chat on a computer the roster
// has not yet listed would otherwise cause.
func TestWithRemoteControlOffAWiredOpenerChangesNothingOnHome(t *testing.T) {
	// The listing has landed and the roster has not, which is the case where an
	// opener that was offered would hold the Enter and ask the roster first.
	build := func(wired bool) (*app, *rosterFleet, *[]chatlist.Row) {
		a, _ := liveHomeRig(t, "dev_tower")
		fleet := &rosterFleet{devices: rosterOf}
		a.fleet = fleet
		a.devRow.devices = nil
		var calls *[]chatlist.Row
		if wired {
			calls, _ = wireFakeOpenLive(t, a, nil)
			a.remoteControl = (&liveSetting{on: false}).read
		}
		return a, fleet, calls
	}
	bare, bareFleet, _ := build(false)
	wired, wiredFleet, calls := build(true)

	standOn(t, bare, "c1")
	standOn(t, wired, "c1")

	if got, want := homeText(wired), homeText(bare); got != want {
		t.Fatalf("the Home frame differs once the opener is wired with remote control off:\n--- wired\n%s\n--- bare\n%s", got, want)
	}
	if wired.home.msg != bare.home.msg {
		t.Fatalf("the foot differs: wired %q, bare %q", wired.home.msg, bare.home.msg)
	}
	if !moveCardRaised(wired) {
		t.Fatal("the Enter did not end in the Move card")
	}
	if len(*calls) != 0 {
		t.Fatalf("remote control off still attempted %d live opens", len(*calls))
	}
	if wiredFleet.asks != bareFleet.asks || wiredFleet.asks != 0 {
		t.Fatalf("the roster was asked %d times (bare %d), want none: an Enter with remote control off waits for nothing", wiredFleet.asks, bareFleet.asks)
	}
}

// C2: the displacement follow, which opens the new holder's view when another
// computer takes this window's chat, is also a live view and is also absent
// while remote control is off.
func TestAHolderIsNotFollowedLiveWhileRemoteControlIsOff(t *testing.T) {
	a, _, setting, _ := settingRig(t, false)
	row := a.machineRead.rows[0]

	if cmd, followed := a.followHolderLive(row); followed || cmd != nil {
		t.Fatal("the window followed a holder live with remote control off")
	}
	setting.on = true
	if _, followed := a.followHolderLive(row); !followed {
		t.Fatal("the window did not follow a holder live with remote control on")
	}
}

// C4: a hosted window never offers live views, whatever the setting says.
func TestAHostedWindowNeverOffersLiveViewsWhateverTheSettingSays(t *testing.T) {
	a, _, _, calls := settingRig(t, true)
	a.host = "devbox"

	if a.liveEligible(a.machineRead.rows[0]) {
		t.Fatal("a hosted window offered a live view with remote control on")
	}
	standOn(t, a, "c1")
	if len(*calls) != 0 || a.liveFront() != nil {
		t.Fatalf("a hosted window opened a live view: opens=%d live=%v", len(*calls), a.liveFront())
	}
}

// A window with an opener and no setting seam offers live views, as every window
// did before the setting was handed in, and a window with a setting seam and no
// opener offers none however the setting reads.
func TestTheOpenerAloneDecidesWhenNoSettingIsHandedIn(t *testing.T) {
	a, _, _, _ := settingRig(t, false)
	a.remoteControl = nil
	if !a.liveOffered() {
		t.Fatal("an opener with no setting seam was not offered")
	}
	a.openLive = nil
	a.remoteControl = (&liveSetting{on: true}).read
	if a.liveOffered() {
		t.Fatal("a window with no opener offered live views because the setting reads on")
	}
}

// Options.RemoteControl is what a window reads: the function handed in at
// construction is the one asked at each Enter.
func TestTheSettingHandedInAtConstructionIsTheOneAsked(t *testing.T) {
	setting := &liveSetting{}
	a := newApp(context.Background(), Options{
		Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/lab", ProfileDir: mintProfileDir(),
		OpenLive:      func(context.Context, chatlist.Row) (Conversation, error) { return Conversation{}, ErrNoLiveView },
		RemoteControl: setting.read,
	})
	if a.liveOffered() {
		t.Fatal("a window built with the setting off offers live views")
	}
	setting.on = true
	if !a.liveOffered() {
		t.Fatal("a window built with the setting off did not follow it switching on")
	}
}
