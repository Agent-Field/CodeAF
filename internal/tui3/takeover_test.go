package tui3

// takeover_test.go is the surface's half of moving a conversation out of the
// terminal that is holding it: home's two enters, the wait on the flock, and the
// holder letting go.
//
// The flocks here are REAL — the same lock a second aforge meets, taken the same
// way (internal/session's sessionfile.go) — because the whole exchange is timed
// against that lock and a flag standing in for it would test nothing.

import (
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// holdUntil takes the journal's flock and hands back the release, for the tests
// whose whole subject is the moment it frees. [homeLab.hold] holds until the
// test ends, which is right for a refusal and useless here.
func (l *homeLab) holdUntil(transcript string) func() {
	l.t.Helper()
	file, err := os.Open(transcript)
	if err != nil {
		l.t.Fatal(err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		l.t.Fatalf("could not hold %s: %v", transcript, err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		unix.Flock(int(file.Fd()), unix.LOCK_UN)
		file.Close()
	}
	l.t.Cleanup(release)
	return release
}

// ── home: the two enters ────────────────────────────────────────────────────

// THE FIRST ENTER ASKS AND THE SECOND ANSWERS. Nothing is written on the first,
// and the line on the foot says both what the next key does and what it costs.
func TestFirstEnterOnAHeldRowArmsItAndSaysWhatTheNextOneDoes(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))

	if a.home.armed != theirs {
		t.Fatalf("the row was not armed · armed=%q", a.home.armed)
	}
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("the first enter already asked the other window for the conversation")
	}
	// The foot line is asserted as the SENTENCE and not as the frame: it is one
	// row and a narrow terminal truncates it, exactly as it truncates every
	// other long refusal on this screen.
	for _, want := range []string{
		"open in another window",
		"enter again to move it here",
		"it moves when that window's reply ends",
		"its tasks resume here",
	} {
		if !strings.Contains(a.home.msg, want) {
			t.Fatalf("the armed line is missing %q:\n%s", want, a.home.msg)
		}
	}
	if !strings.Contains(homeText(a), "enter again to move it here") {
		t.Fatalf("the offer never reached the screen:\n%s", homeText(a))
	}
	if !a.at(pageHome) {
		t.Fatal("arming a row closed home")
	}
}

// THE SECOND ENTER WRITES THE REQUEST AND WAITS. Nothing is opened yet — the
// other window still holds the journal, and it answers when its reply ends.
func TestSecondEnterAsksForTheConversationAndWaits(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	opened := 0
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened++
		return Conversation{Agent: &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
			SessionFile: transcript, Workspace: workspace, Resumed: true}, nil
	}
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))

	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err != nil {
		t.Fatalf("no request was left for the other window: %v", err)
	}
	if opened != 0 {
		t.Fatalf("the row was opened while the other window still held it (%d times)", opened)
	}
	if !a.waitingToTakeOver() {
		t.Fatal("the surface is not waiting for the conversation it asked for")
	}
	if !strings.Contains(homeText(a), takeoverWaitWord) {
		t.Fatalf("the waiting line is not on the screen:\n%s", homeText(a))
	}
	if !a.at(pageHome) {
		t.Fatal("asking for the conversation closed home")
	}
}

// AND WHEN THE OTHER WINDOW LETS GO, THE ROW OPENS BY THE ORDINARY DOOR.
func TestTheRowOpensTheMomentTheOtherWindowLetsGo(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	release := lab.holdUntil(theirs)

	a := lab.app(mine)
	opened := 0
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened++
		return Conversation{Agent: &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
			SessionFile: transcript, Workspace: workspace, Resumed: true}, nil
	}
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))

	// One beat while it is still held changes nothing but the line.
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
	if opened != 0 || !a.waitingToTakeOver() {
		t.Fatal("a beat taken while the journal was still held opened the row")
	}

	release()
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})

	if opened != 1 {
		t.Fatalf("the freed row was opened %d times, want once", opened)
	}
	if a.waitingToTakeOver() {
		t.Fatal("the surface is still waiting for a conversation it has")
	}
	if a.at(pageHome) {
		t.Fatalf("the conversation arrived and home stayed up · %s", a.home.msg)
	}
	if a.file != theirs {
		t.Fatalf("the window landed on %q, want %q", a.file, theirs)
	}
}

// A LONG WAIT GROWS ITS REASON, and it never grows a deadline: the other window
// is finishing a reply, and cutting one is the whole thing this design refuses.
func TestALongWaitSaysWhyAndOffersTheKeyThatEndsIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))
	a.takeover.since = a.now().Add(-takeoverPatience - time.Second)

	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
	if got := a.home.msg; got != takeoverStillWord {
		t.Fatalf("a long wait says %q", got)
	}
	if !a.waitingToTakeOver() {
		t.Fatal("the wait gave up on its own")
	}
}

// ESC STOPS WAITING, and it takes the request off the disk so the other window
// never answers a question nobody is listening for.
func TestEscStopsWaitingAndWithdrawsTheRequest(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))

	a.homeKey(key("esc"))
	if a.waitingToTakeOver() {
		t.Fatal("esc left the window waiting")
	}
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("the withdrawn request is still on the disk")
	}
	if !a.at(pageHome) {
		t.Fatal("the esc that stopped the wait also left home")
	}
	if a.home.msg != "" {
		t.Fatalf("home is still saying %q about a wait that ended", a.home.msg)
	}
	// A STALE BEAT AFTER A CANCEL DOES NOTHING AT ALL.
	if cmd := a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen - 1}); cmd != nil {
		t.Fatal("a beat from the cancelled wait kept beating")
	}
}

// MOVING THE CURSOR DISARMS. An arming a person can no longer see would turn the
// next enter into a key that ends another window.
func TestMovingOffAHeldRowDisarmsTheMove(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	if a.home.armed == "" {
		t.Fatal("the row was not armed to begin with")
	}
	a.homeKey(key("down"))
	if a.home.armed != "" {
		t.Fatalf("the row is still armed after the cursor moved · %q", a.home.armed)
	}
	// And enter on it again ASKS AGAIN rather than moving anything.
	a.home.point(theirs)
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("a re-armed row asked for the conversation on the first press")
	}
}

// OVER --host THERE IS NOBODY TO ASK: the holder is a window on this laptop and
// the journal is on the far machine. The old refusal stands, unchanged.
func TestOverHostAHeldRowKeepsTheOldRefusal(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	// The screen is built LOCALLY and the machine is set after, because a hosted
	// home reads the far machine's world and this lab is this machine's disk —
	// what is under test is the door the key reaches, not where the rows came
	// from.
	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	line, ok := a.home.focusedLine()
	if !ok || line.row.Transcript != theirs {
		t.Fatal("the cursor is not on the held row")
	}
	a.host = "devbox"
	a.homeOpenLine(line)

	if a.home.msg != sessionBusyWord {
		t.Fatalf("a hosted home said %q", a.home.msg)
	}
	if a.home.armed != "" {
		t.Fatal("a hosted home armed a row it cannot move")
	}
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("a hosted home wrote a request onto this machine's disk")
	}
}

// ── the launch that met a lock ──────────────────────────────────────────────

// A LAUNCH THAT COULD NOT OPEN THE CONVERSATION LANDS ON IT, ARMED, so one enter
// continues it rather than leaving somebody with a second conversation.
func TestALaunchThatMetALockLandsOnThatRowArmed(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	fresh := lab.session("-tmp-alpha", "aaaa000000000001", "the one the door built", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(fresh)
	a.landTakeover(theirs)

	if !a.at(pageHome) {
		t.Fatal("a launch that met a lock did not land on home")
	}
	line, ok := a.home.focusedLine()
	if !ok || line.row.Transcript != theirs {
		t.Fatalf("home opened on %+v, want the row it could not open", line.row.Transcript)
	}
	if a.home.armed != theirs {
		t.Fatalf("the row is not armed · %q", a.home.armed)
	}
	if !strings.Contains(homeText(a), "enter again to move it here") {
		t.Fatalf("the offer is not on the screen:\n%s", homeText(a))
	}
	// AND ONE ENTER ASKS FOR IT.
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err != nil {
		t.Fatalf("the armed row did not ask on its first enter: %v", err)
	}
}

// ── the holder ──────────────────────────────────────────────────────────────

// THE WINDOW HOLDING IT LETS GO, and lands on a fresh conversation when it was
// holding nothing else.
func TestTheHolderLetsGoAndLandsOnAFreshConversation(t *testing.T) {
	lab := newHomeLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", lab.project("-tmp-alpha"), time.Now())

	a := lab.app(mine)
	held := a.agent.(*fakeAgent)
	a.taskEvent(session.Event{Kind: session.EventTakeover, Text: session.TakeoverWord})

	if held.closes != 1 {
		t.Fatalf("the conversation was closed %d times, want once", held.closes)
	}
	if a.agent == Agent(held) {
		t.Fatal("the window is still holding the conversation another window asked for")
	}
	if said := homeNotes(a); !strings.Contains(said, session.TakeoverWord) {
		t.Fatalf("the window said %q about letting go", said)
	}
}

// AND WHEN IT WAS HOLDING SOMETHING ELSE, that one comes forward.
func TestTheHolderLetsGoOntoTheConversationItWasKeeping(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	free := lab.session("-tmp-alpha", "aaaa000000000002", "the other one", where, now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	a.home.point(free)
	a.homeKey(key("enter"))
	if a.file != free || len(a.behind) != 1 {
		t.Fatalf("the lab did not end up with one conversation in front and one kept · %q %d", a.file, len(a.behind))
	}
	front := a.agent
	a.taskEvent(session.Event{Kind: session.EventTakeover, Text: session.TakeoverWord})

	if a.agent == front {
		t.Fatal("the window is still holding the conversation another window asked for")
	}
	if a.file != mine {
		t.Fatalf("the window landed on %q, want the conversation it was keeping", a.file)
	}
	if len(a.behind) != 0 {
		t.Fatalf("the keeper still holds %d", len(a.behind))
	}
}

// A CONVERSATION THIS WINDOW IS KEEPING BUT NOT DRAWING is let go of on its own
// stir, with the banner naming it rather than a note in a transcript about
// something else.
func TestAKeptConversationIsLetGoOfOnItsStir(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	free := lab.session("-tmp-alpha", "aaaa000000000002", "the other one", where, now.Add(-time.Hour))

	a := lab.app(mine)
	a.stirLane()
	a.openHome()
	a.home.point(free)
	a.homeKey(key("enter"))
	key := a.convKey(mine)
	held := a.behind[key]
	if held == nil {
		t.Fatal("the conversation this window left is not in the keeper")
	}
	kept := held.conv.Agent

	held.watch.takeover.Store(true)
	a.behindStir(key)

	if a.behind[key] != nil {
		t.Fatal("the kept conversation was not let go of")
	}
	if agent, ok := kept.(*fakeAgent); ok && agent.closes != 1 {
		t.Fatalf("the kept conversation was closed %d times, want once", agent.closes)
	}
	if a.file != free {
		t.Fatalf("letting go of a kept conversation moved the window to %q", a.file)
	}
}
