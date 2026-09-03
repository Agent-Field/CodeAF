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

	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
)

// cardSays reports that a phrase is on the card, ACROSS ITS WRAPS. The right
// column is thirty-six cells at its narrowest and every sentence longer than
// that is laid over two rows ([wrap]), so an assertion that looked line by line
// would be testing the width of the terminal rather than the words on it.
func cardSays(card []string, phrase string) bool {
	return strings.Contains(strings.Join(strings.Fields(strings.Join(card, " ")), " "),
		strings.Join(strings.Fields(phrase), " "))
}

// holdUntil takes the journal's flock and hands back the release, for the tests
// whose whole subject is the moment it frees. [homeLab.hold] holds until the
// test ends, which is right for a refusal and useless here.
func (l *homeLab) holdUntil(transcript string) func() {
	l.t.Helper()
	file, err := os.Open(transcript)
	if err != nil {
		l.t.Fatal(err)
	}
	if err := filelock.Lock(file, true, true); err != nil {
		file.Close()
		l.t.Fatalf("could not hold %s: %v", transcript, err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		filelock.Unlock(file)
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
	// AND THE CARD SAYS IT WHERE THE EYE IS. The foot keeps the long sentence
	// because this is the one press that ends another window; the card carries
	// the same offer beside the row it is about, in its own words.
	card := homeCardFor(t, a, theirs)
	for _, want := range []string{takeoverAgainWord, takeoverCostWords[0], takeoverCostWords[1]} {
		if !cardSays(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, strings.Join(card, "\n"))
		}
	}
}

// AT REST THE CARD CARRIES THE DOOR, three cells from the row it is about. The
// whole complaint in the report was that the only account of this door was a
// dim line at the far end of the frame from the thing it described.
func TestAHeldRowsCardNamesTheDoorAtRest(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.openHome()
	card := homeCardFor(t, a, theirs)
	if !cardSays(card, homeHeldWord) {
		t.Fatalf("the card no longer says where the conversation is:\n%s", strings.Join(card, "\n"))
	}
	if !cardSays(card, takeoverDoorWord) {
		t.Fatalf("the card names no way to bring it here:\n%s", strings.Join(card, "\n"))
	}
	// AND A CONVERSATION NOBODY IS HOLDING GETS NONE OF IT. The door only exists
	// where there is a window to ask.
	free := homeCardFor(t, a, mine)
	if cardSays(free, takeoverDoorWord) {
		t.Fatalf("a free conversation was offered a move:\n%s", strings.Join(free, "\n"))
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
	// THE ROW SAYS IT, in the margin every other row of this surface says what
	// it is in. This is the defect the report was actually about: a claim that
	// rendered nothing anywhere is indistinguishable from a key that did not
	// work.
	if !strings.Contains(homeText(a), takeoverComingWord) {
		t.Fatalf("nothing on the screen says the conversation is coming:\n%s", homeText(a))
	}
	if !strings.Contains(homeText(a), "esc") {
		t.Fatalf("the screen names no way out of the wait:\n%s", homeText(a))
	}
	if !a.at(pageHome) {
		t.Fatal("asking for the conversation closed home")
	}
}

// AND THE ROW'S OWN WORD REPLACES `another window`, because a person who has
// just pressed enter is asking whether it is coming and not where it is.
func TestTheClaimedRowSaysItIsComingAndTakesTheOneSpinner(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now)

	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	if row, ok := a.home.focusedLine(); !ok || homeNote(row.row, a.homeHeld(row.row), "", a.takeoverRowWord(row.row),
		a.homeMark(row.row), false, 0, now) == "" {
		t.Fatal("the held row carries no note at all")
	}
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))

	line, ok := a.home.focusedLine()
	if !ok {
		t.Fatal("the cursor left the row it claimed")
	}
	if got := a.takeoverRowWord(line.row); got != takeoverComingWord {
		t.Fatalf("the claimed row's margin says %q, want %q", got, takeoverComingWord)
	}
	// THE ONE SPINNER LANDS ON IT. A claim is by construction the most recent
	// thing anybody did on this machine, which is that law's own rule for which
	// row moves (homespinner.go).
	a.home.build()
	if at := a.home.spinAt(); at < 0 || a.home.lines[at].row.Transcript != theirs {
		t.Fatalf("the moving cell is on line %d, not on the conversation coming here", at)
	}
	if !a.homeAnimating() {
		t.Fatal("home is not animating while a conversation is on its way to it")
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

// claimHeld arms and asks for a row another window is holding, and hands back
// the surface sitting on the wait. The frame is widened to the card tier first,
// because the card is where this door now says everything it has to say.
func claimHeld(t *testing.T, lab *homeLab, mine, theirs string) *app {
	t.Helper()
	a := lab.app(mine)
	a.width, a.height = homeCardMin, 40
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	a.homeKey(key("enter"))
	if !a.waitingToTakeOver() {
		t.Fatal("the two enters left no claim out")
	}
	return a
}

// A WAIT ON A WINDOW THAT IS MID-REPLY SAYS SO, IN INK, ON THE CARD. It never
// grows a deadline: the other window is finishing a reply, and cutting one is
// the whole thing this design refuses.
func TestAWaitOnAWindowMidReplySaysSoBesideTheRow(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", now)

	a := claimHeld(t, lab, mine, theirs)
	a.takeover.since = a.now().Add(-takeoverPatience - time.Second)

	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
	if !a.waitingToTakeOver() {
		t.Fatal("the wait gave up on its own")
	}
	card := homeCardFor(t, a, theirs)
	for _, want := range []string{takeoverComingWord, takeoverMidReplyWord, takeoverStopWord} {
		if !cardSays(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, strings.Join(card, "\n"))
		}
	}
	// AND IT DOES NOT ALSO INVENT A SECOND REASON. `has not answered yet` is
	// what a quiet window gets; a window that is mid-reply has answered as fast
	// as this design lets it.
	if cardSays(card, takeoverQuietWord) {
		t.Fatalf("the card gave two reasons for one wait:\n%s", strings.Join(card, "\n"))
	}
	// THE FOOT IS QUIET WHILE THE CARD IS UP. One fact, one place.
	if a.home.msg != "" {
		t.Fatalf("the foot repeated the card: %q", a.home.msg)
	}
}

// A WAIT ON A WINDOW WITH NOTHING IN FLIGHT SAYS NOTHING IT DOES NOT KNOW —
// until it has gone on long enough that the silence is itself the news.
func TestAQuietWaitStaysQuietAndThenSaysNobodyAnswered(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceIdle, "", now)

	a := claimHeld(t, lab, mine, theirs)
	card := homeCardFor(t, a, theirs)
	if !cardSays(card, takeoverComingWord) {
		t.Fatalf("a fresh claim says nothing:\n%s", strings.Join(card, "\n"))
	}
	for _, banned := range []string{takeoverMidReplyWord, takeoverQuietWord} {
		if cardSays(card, banned) {
			t.Fatalf("an idle window's move was given a reason it does not have (%q):\n%s",
				banned, strings.Join(card, "\n"))
		}
	}
	// THE EMPTINESS LAW ON A CLOCK: a move that has taken no time says no time.
	if cardSays(card, "0s") {
		t.Fatalf("the card drew a zero:\n%s", strings.Join(card, "\n"))
	}

	a.takeover.since = a.now().Add(-takeoverPatience - time.Second)
	card = homeCardFor(t, a, theirs)
	if !cardSays(card, takeoverQuietWord) {
		t.Fatalf("a long quiet wait explained nothing:\n%s", strings.Join(card, "\n"))
	}
	if !cardSays(card, reltime.Elapsed(takeoverPatience+time.Second)) {
		t.Fatalf("the card never said how long it had been:\n%s", strings.Join(card, "\n"))
	}
}

// A REQUEST NOBODY EVER ANSWERS ENDS, AND SAYS SO. Past [session.TakeoverStale]
// the holder deletes it unread, so a window still beating at the flock is
// waiting for something that cannot now happen — which is what this one used to
// do, for ever, in silence.
func TestAClaimThatAgesOutStopsAndSaysTheOtherWindowStillHasIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := claimHeld(t, lab, mine, theirs)
	a.takeover.since = a.now().Add(-session.TakeoverStale - time.Second)

	if cmd := a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen}); cmd != nil {
		t.Fatal("a claim nothing will answer kept beating")
	}
	if a.waitingToTakeOver() {
		t.Fatal("the surface is still waiting for a request that has aged out")
	}
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("the dead request was left in the other window's folder")
	}
	card := homeCardFor(t, a, theirs)
	if !cardSays(card, takeoverUnansweredWord) {
		t.Fatalf("the wait ended in silence:\n%s", strings.Join(card, "\n"))
	}
	if !cardSays(card, takeoverRetryWord) {
		t.Fatalf("the card names no way to ask again:\n%s", strings.Join(card, "\n"))
	}
	// AND THE KEY IT NAMES IS THE KEY IT MEANS. `enter asks again` is one press
	// and not two: this row has been through the two-key door already.
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err != nil {
		t.Fatalf("`enter asks again` asked nothing: %v", err)
	}
	if !a.waitingToTakeOver() {
		t.Fatal("asking again left no claim out")
	}
}

// A CONVERSATION THAT CAME FREE WHILE NOBODY WAS ON HOME IS NEWS WHEN THEY COME
// BACK. Nothing is opened under somebody who walked away — that rule stands —
// but the ending used to be dropped on the floor with it.
func TestAConversationThatCameFreeWhileAwayIsSaidWhenHomeComesBack(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	release := lab.holdUntil(theirs)

	a := claimHeld(t, lab, mine, theirs)
	opened := 0
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened++
		return Conversation{Agent: &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
			SessionFile: transcript, Workspace: workspace, Resumed: true}, nil
	}
	a.closeHome()
	release()
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})

	if opened != 0 {
		t.Fatal("a conversation was opened under somebody who had walked away")
	}
	a.openHome()
	card := homeCardFor(t, a, theirs)
	if !cardSays(card, takeoverFreeWord) {
		t.Fatalf("home said nothing about the conversation that came free:\n%s", strings.Join(card, "\n"))
	}
}

// THE LAW: A CLAIM IS NEVER SILENT. Whatever state it is in, the screen says
// something about it — which is the one thing the surface did not do before.
func TestNoStateOfAMoveIsSilent(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)
	lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceIdle, "", now)

	a := lab.app(mine)
	a.width, a.height = homeCardMin, 40
	a.openHome()
	a.home.point(theirs)

	// rest, armed, moving, mid-reply, aged out — every state this door has.
	for _, stage := range []struct {
		word string
		set  func()
	}{
		{"at rest", func() {}},
		{"armed", func() { a.homeKey(key("enter")) }},
		{"moving", func() { a.homeKey(key("enter")) }},
		{"mid-reply", func() {
			lab.presence("-tmp-alpha", "aaaa000000000002", session.PresenceWorking, "", a.now())
			a.refreshHome()
		}},
		{"aged out", func() {
			a.takeover.since = a.now().Add(-session.TakeoverStale - time.Second)
			a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})
		}},
	} {
		stage.set()
		card := homeCardFor(t, a, theirs)
		spoke := false
		for _, word := range []string{takeoverComingWord, takeoverDoorWord, takeoverAgainWord, takeoverUnansweredWord} {
			if cardSays(card, word) {
				spoke = true
			}
		}
		if !spoke {
			t.Fatalf("the card says nothing at all about a move that is %q:\n%s",
				stage.word, strings.Join(card, "\n"))
		}
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

// ── THE DOOR ON THE ROW THE CURSOR IS ON ────────────────────────────────────

// homeRowFor is the one drawn line of home's list that names this title, with
// the colour taken off it. The tests below are about the RIGHT MARGIN of one
// row, so they need that row and not the whole screen: `another window` appears
// on every held row and the assertions are about which of them grew.
func homeRowFor(t *testing.T, a *app, title string) string {
	t.Helper()
	for _, line := range strings.Split(homeText(a), "\n") {
		if strings.Contains(line, title) {
			return line
		}
	}
	t.Fatalf("no row named %q on the screen:\n%s", title, homeText(a))
	return ""
}

// THE HELD ROW UNDER THE CURSOR SAYS HOW TO GET IT BACK, AND ITS SIBLINGS DO
// NOT. This is the whole of the report: `another window` names where a
// conversation is and nothing at all about the way back, and the way back was
// told only on the card three cells away and on the foot line thirty rows down.
//
// The margin grows on ONE row because the eye is on one row — and because the
// sentence is only true of the row enter would act on. Seven held rows each
// repeating the same instruction is not seven answers.
func TestTheHeldRowUnderTheCursorNamesTheDoorAndItsSiblingsDoNot(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.workspace("alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	third := lab.session("-tmp-alpha", "aaaa000000000003", "a third window", where, now.Add(-2*time.Hour))
	lab.hold(theirs)
	lab.hold(third)

	a := lab.app(mine)
	a.width, a.height = homeSwitchFull, 30
	a.openHome()
	a.home.point(theirs)

	if got := homeRowFor(t, a, "The Other Terminal"); !strings.Contains(got, takeoverHeldDoorWord) {
		t.Fatalf("the held row under the cursor is %q, want it to name the door", strings.TrimSpace(got))
	}
	other := homeRowFor(t, a, "A Third Window")
	if !strings.Contains(other, homeHeldShort) {
		t.Fatalf("a held row lost its own word: %q", strings.TrimSpace(other))
	}
	if strings.Contains(other, takeoverDoorWord) {
		t.Fatalf("a held row nobody is standing on offered the door: %q", strings.TrimSpace(other))
	}
	// AND THE SENTENCE FOLLOWS THE CURSOR rather than sticking to the row it
	// was first drawn on.
	a.home.point(third)
	if got := homeRowFor(t, a, "A Third Window"); !strings.Contains(got, takeoverHeldDoorWord) {
		t.Fatalf("the door did not follow the cursor: %q", strings.TrimSpace(got))
	}
	if got := homeRowFor(t, a, "The Other Terminal"); strings.Contains(got, takeoverDoorWord) {
		t.Fatalf("the row the cursor left kept the door: %q", strings.TrimSpace(got))
	}
}

// AND A ROW WITH NO ROOM FOR THE SENTENCE HANDS IT BACK WHOLE. Giving way is
// dropping the clause, never cutting it: `another window · enter brings i` costs
// the name its cells and buys an instruction nobody can follow.
//
// The name is what the clause may not spend. This list's job is choosing between
// conversations, so the growth is offered only where the whole title still fits
// beside it — and the card carries the door at every width, which is what makes
// the row's silence affordable.
func TestTheDoorOnTheMarginGivesWayToTheNameRatherThanCutAWord(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.workspace("alpha")
	long := "a conversation whose name is long enough to want every cell this row has to give"
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", long, where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.width, a.height = homeCardMin, 30
	a.openHome()
	a.home.point(theirs)

	row := homeRowFor(t, a, "Long Enough")
	if !strings.Contains(row, homeHeldShort) {
		t.Fatalf("the narrow row lost where the conversation is: %q", strings.TrimSpace(row))
	}
	if strings.Contains(row, takeoverDoorWord) {
		t.Fatalf("the row grew a sentence it had no room for: %q", strings.TrimSpace(row))
	}
	// NOTHING HALF-SAID. A row that cut the clause would leave a prefix of it
	// behind, and that is the failure this test is actually about.
	for _, half := range []string{"enter bring", "brings it", "· enter"} {
		if strings.Contains(row, half) {
			t.Fatalf("the margin was cut mid-sentence at %q: %q", half, strings.TrimSpace(row))
		}
	}
	// AND THE CARD STILL CARRIES IT, which is why the row may go quiet.
	if card := homeCardFor(t, a, theirs); !cardSays(card, takeoverDoorWord) {
		t.Fatalf("the card lost the door the row gave up:\n%s", strings.Join(card, "\n"))
	}
}

// A ROW WITH NO DOOR IS NEVER OFFERED ONE. Over --host the window holding the
// conversation is on this laptop and the journal is on the far machine, so there
// is nobody to ask; a conversation with no folder of its own has nowhere to
// leave a request. Both are `another window` and neither has a way back, and a
// margin that named a key it would then refuse is the worst thing a word on a
// door can do.
func TestNoDoorOnTheMarginWhereEnterWouldRefuse(t *testing.T) {
	now := time.Date(2026, time.September, 1, 13, 0, 0, 0, time.UTC)
	held := session.SessionRow{ID: "held", Dir: "/state/alpha/held", Transcript: "/state/alpha/held/t.jsonl",
		Project: "alpha", Title: "Held chat", At: now.Add(-time.Hour), Open: true}
	flat := session.SessionRow{ID: "flat", Transcript: "/old/flat.jsonl",
		Project: "alpha", Title: "Flat chat", At: now.Add(-time.Hour), Open: true}
	world := session.World{Read: now, Projects: []session.Project{
		{Bucket: "alpha", Dir: "/state/alpha", Path: "/work/alpha", Name: "alpha",
			Sessions: []session.SessionRow{held, flat}}}}

	local := readSwitcher(world, nil, switcherHere{}, nil, time.Time{}, now, switcherView{}, switcherLedgerInput{})
	for _, row := range switcherStops(local) {
		if !row.held {
			t.Fatalf("%q is not held and this test needs it to be", row.title)
		}
		if row.session.ID == "flat" && row.door {
			t.Fatal("a conversation with no folder of its own was offered a move")
		}
		if row.session.ID == "held" && !row.door {
			t.Fatal("an ordinary held conversation lost its door")
		}
	}
	far := readSwitcher(world, nil, switcherHere{hosted: true}, nil, time.Time{}, now, switcherView{}, switcherLedgerInput{})
	for _, row := range switcherStops(far) {
		if row.door {
			t.Fatalf("%q was offered a move on another machine's home", row.title)
		}
	}
}
