package tui3

// takeover_test.go is the surface's half of moving a conversation out of the
// terminal that is holding it: home's two enters, the wait on the flock, and the
// holder letting go.
//
// The flocks here are REAL — the same lock a second aforge meets, taken the same
// way (internal/session's sessionfile.go) — because the whole exchange is timed
// against that lock and a flag standing in for it would test nothing.

import (
	"context"
	"os"
	"slices"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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

// moveItHere is the whole door, driven the way a person drives it: enter raises
// the question, `1` moves the cursor onto `move it here` — which answers nothing
// — and enter takes it (takeover.go's card).
//
// IT IS THREE KEYS AND NOT TWO ON PURPOSE. The tests below are about what
// happens AFTER the request is on the disk, and every one of them used to spend
// two lines spelling `enter` twice; the door's own grammar is asserted by
// [TestEnterOnAHeldRowRaisesTheQuestionWithTheSafeAnswerUnderTheCursor] and
// [TestTheDigitMovesTheCursorAndEnterMovesTheConversation], which is where a
// change to it should go red.
func moveItHere(a *app) {
	a.homeKey(key("enter"))
	a.homeKey(key("1"))
	a.homeKey(key("enter"))
}

// ── home: enter raises the question ─────────────────────────────────────────

// THE FIRST ENTER RAISES THE QUESTION AND ANSWERS NOTHING. Nothing is written
// on it, the card home draws is the question block's own, and the cursor is on
// the answer that loses nothing.
func TestEnterOnAHeldRowRaisesTheQuestionWithTheSafeAnswerUnderTheCursor(t *testing.T) {
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
	ask, up := a.homeAsking()
	if !up {
		t.Fatal("enter armed the row and asked nothing")
	}
	if ask.question.Ask != session.AskConfirmation {
		t.Fatalf("the question is a %q and not a confirmation", ask.question.Ask)
	}
	// THE CURSOR IS ON THE ANSWER THAT LOSES NOTHING, which is the whole reason
	// this door became a card: enter is the key people press to make a question
	// go away, and the thing it must never do here is end another window.
	if ask.pick != takeoverStayAt {
		t.Fatalf("the cursor opened on answer %d, not on %q", ask.pick, takeoverStayWord)
	}
	// AND THE CARD SAYS IT WHERE THE EYE IS, beside the row it is about, with
	// what each answer costs on the answer rather than on a foot thirty rows
	// away.
	card := homeCardFor(t, a, theirs)
	for _, want := range []string{takeoverAskWord, takeoverMoveWord, takeoverStayWord} {
		if !cardSays(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, strings.Join(card, "\n"))
		}
	}
	// WHAT MOVING IT COSTS IS ON THE CARD, in the reason row under the question
	// — which is where it fits on a card home lends fifty columns.
	if !cardSays(card, takeoverCostWord) {
		t.Fatalf("the card does not say what moving it costs:\n%s", strings.Join(card, "\n"))
	}
	// AND WHERE THE CONVERSATION IS IS NOT SAID TWICE. The row's own margin and
	// the line above the card already carry it; a reason row repeating it would
	// be the same sentence in two hues on one card.
	if strings.Count(strings.Join(card, "\n"), homeHeldWord) > 1 {
		t.Fatalf("the card says where it is more than once:\n%s", strings.Join(card, "\n"))
	}
	if !a.at(pageHome) {
		t.Fatal("arming a row closed home")
	}

	// A SECOND ENTER TAKES WHAT THE CURSOR IS ON, which is `leave it there`: the
	// request is still not on the disk and the card is gone.
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("leaning on enter moved the conversation")
	}
	if _, up := a.homeAsking(); up {
		t.Fatal("the card is still up after it was answered")
	}
	if a.home.armed != "" {
		t.Fatalf("the row is still armed after `%s` · armed=%q", takeoverStayWord, a.home.armed)
	}
}

// AND `1` THEN ENTER IS THE MOVE. The digit names the answer and moves the
// cursor onto it; enter is what takes it, which is one grammar with every other
// confirmation on this surface.
func TestTheDigitMovesTheCursorAndEnterMovesTheConversation(t *testing.T) {
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
	a.homeKey(key("1"))

	ask, up := a.homeAsking()
	if !up {
		t.Fatal("the digit answered the question outright")
	}
	if ask.pick != 0 {
		t.Fatalf("`1` did not move the cursor onto %q · pick=%d", takeoverMoveWord, ask.pick)
	}
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("the digit asked the other window for the conversation")
	}
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err != nil {
		t.Fatalf("enter on `%s` wrote no request: %v", takeoverMoveWord, err)
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
	moveItHere(a)

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
	moveItHere(a)

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
	moveItHere(a)

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
	moveItHere(a)
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
	// AND THE KEY IT NAMES RAISES THE QUESTION AGAIN, exactly as it does on a
	// row that has never been asked about. A row that had been through the door
	// once used to get the ask on a single press — a shortcut that meant one
	// keystroke could end another window, on the one row where somebody has
	// already pressed enter twice and learnt it does nothing.
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("`enter asks again` asked the other window on one press")
	}
	if _, up := a.homeAsking(); !up {
		t.Fatal("`enter asks again` raised no question")
	}
	a.homeKey(key("1"))
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
		{"moving", func() { a.homeKey(key("1")); a.homeKey(key("enter")) }},
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
		for _, word := range []string{takeoverComingWord, takeoverDoorWord, takeoverAskWord, takeoverUnansweredWord} {
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
	moveItHere(a)

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
	if !strings.Contains(homeText(a), takeoverAskWord) {
		t.Fatalf("the question is not on the screen:\n%s", homeText(a))
	}
	// AND THE QUESTION IS ALREADY ASKED, WITH THE CURSOR ON `leave it there`. A
	// launch that met a lock may not be a launch that MOVES the conversation on
	// the first keystroke — the person did not ask for this screen, they asked
	// for a conversation, and the key they will press to get on with it is enter.
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err == nil {
		t.Fatal("enter on the landed question moved the conversation")
	}
	moveItHere(a)
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err != nil {
		t.Fatalf("the landed question could not be answered `%s`: %v", takeoverMoveWord, err)
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
	a.behindStir(behindStirMsg{key: key})

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

// ── the arriving window: a question nobody answered ─────────────────────────

// resumingAgent is a conversation whose journal ends on a question this machine
// stopped answering (session's resume.go). It answers the one door the surface
// asks about that, and counts how many times it was asked.
type resumingAgent struct {
	*switchAgent
	asked  int
	answer string
}

func (r *resumingAgent) ResumeStoppedTurn(context.Context) (<-chan session.Event, bool) {
	r.asked++
	if r.asked > 1 {
		return nil, false
	}
	out := make(chan session.Event, 4)
	out <- session.Event{Kind: session.EventTextDelta, Text: r.answer}
	out <- session.Event{Kind: session.EventTurnDone}
	close(out)
	return out, true
}

// A CONVERSATION THAT ARRIVES ON A QUESTION NOBODY ANSWERED ASKS IT AGAIN HERE.
// This is the whole of the repair a person sees: they moved the conversation,
// and the reply they were waiting for starts in front of them without a
// keystroke. Nothing of their question is typed or drawn a second time.
func TestAConversationArrivingOnAnUnansweredQuestionAsksItAgain(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	release := lab.holdUntil(theirs)

	a := lab.app(mine)
	arriving := &resumingAgent{
		switchAgent: &switchAgent{fakeAgent: &fakeAgent{model: "m"}},
		answer:      "the answer nobody had to ask for twice",
	}
	a.open = func(workspace, transcript string) (Conversation, error) {
		return Conversation{Agent: arriving, SessionFile: transcript, Workspace: workspace, Resumed: true}, nil
	}
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	// The card home raises has the cursor on `leave it there`; `1` moves it onto
	// `bring it here` and the second enter answers (homeconfirm.go).
	a.homeKey(key("1"))
	a.homeKey(key("enter"))
	release()
	drive(t, a, takeoverTickMsg{gen: a.takeover.gen})

	if a.file != theirs {
		t.Fatalf("the window landed on %q, want %q", a.file, theirs)
	}
	if arriving.asked != 1 {
		t.Fatalf("the arriving conversation was asked to resume %d times, want once", arriving.asked)
	}
	// ONE DIM LINE SAYS WHY A REPLY STARTED ON ITS OWN, and it is the engine's
	// sentence rather than a second spelling of it.
	said := noteTexts(a)
	if !slices.Contains(said, session.ResumedWord) {
		t.Fatalf("nothing said why the reply started again; the notes were %q", said)
	}
	// AND THE ANSWER ARRIVED.
	if !strings.Contains(transcriptText(a), arriving.answer) {
		t.Fatalf("the resumed reply never reached the page:\n%s", transcriptText(a))
	}
}

// AND THE REQUEST COMES OFF THE DISK WHEN THE CLAIM SUCCEEDS. A lock can free
// for its own reasons — the other window quit, or went to another conversation —
// with this window's question still lying in the folder it is about to open; the
// session opened on it would find that question on its own beat and let go of a
// conversation nobody asked it to.
func TestAClaimThatSucceedsLeavesNoQuestionBehind(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	release := lab.holdUntil(theirs)

	a := lab.app(mine)
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))
	// The card home raises has the cursor on `leave it there`; `1` moves it onto
	// `bring it here` and the second enter answers (homeconfirm.go).
	a.homeKey(key("1"))
	a.homeKey(key("enter"))
	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); err != nil {
		t.Fatalf("no request was left for the other window: %v", err)
	}

	release()
	a.takeoverTick(takeoverTickMsg{gen: a.takeover.gen})

	if _, err := os.Stat(session.TakeoverPath(homeSessionDirOf(theirs))); !os.IsNotExist(err) {
		t.Fatal("the window opened the conversation and left its own question in the folder")
	}
}

// AND THEN SOMEBODY LOOKS AT IT.
//
// The card home raises about moving a conversation is the question block's own
// card drawn inside a home band, which is a place no other screen in this suite
// has photographed. It is written through the same door every question screen
// is (questionscreens_test.go): the whole frame, at truecolor, with the
// geometric glyph floor, and only when AFORGE_SCREENS names a directory.
func TestTheTakeoverCardScreen(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv("AFORGE_SCREENS"))
	lab := newHomeLab(t)
	now := time.Date(2026, time.September, 9, 14, 2, 0, 0, time.UTC)
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.pal = newPalette(tokens.TrueColor, false)
	a.actionAuto = tokens.Plain
	a.settleIcons()
	a.width, a.height = 160, 24
	a.clock = func() time.Time { return now }
	a.openHome()
	a.home.point(theirs)

	shot := func(name string) {
		t.Helper()
		width, height := a.size()
		lines, _, _, _ := a.homeFrame(width, height)
		body := strings.Join(lines, "\n")
		if !strings.Contains(ansi.Strip(body), takeoverAskWord) {
			t.Fatalf("%s does not carry the question:\n%s", name, ansi.Strip(body))
		}
		if dir == "" {
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.homeKey(key("enter"))
	shot("takeover-card")
	// AND THE SAME CARD AFTER `1`, which moves the cursor onto the act and
	// answers nothing — the one thing about this door a sentence cannot show.
	a.homeKey(key("1"))
	shot("takeover-card-picked")
}
