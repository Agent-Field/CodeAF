package tui3

// moved_test.go is the ENGINE ROAD: enter on a conversation an engine is holding
// opens it here at once, and the window it left steps back without ending
// anybody's work.
//
// The flocks here are REAL, exactly as takeover_test.go's are, because the whole
// point is which of the two doors a locked journal goes through.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ENTER ON AN ENGINE-HELD ROW IS THE ORDINARY OPEN. No arm, no confirmation, no
// request written to the disk: the engine hands back the conversation it is
// already running, and home closes into it.
func TestEnterOnAnEngineHeldRowOpensItAndNeverArms(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	asked := ""
	a.engineAnswers = func(workspace string) bool {
		asked = workspace
		return true
	}
	opened := ""
	door := a.open
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened = transcript
		return door(workspace, transcript)
	}
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))

	if asked != where {
		t.Fatalf("the engine was asked about %q, want the row's own project %q", asked, where)
	}
	if opened != theirs {
		t.Fatalf("the ordinary door opened %q, want %q", opened, theirs)
	}
	if a.home.armed != "" {
		t.Fatalf("an engine-held row was armed for a take-over: %q", a.home.armed)
	}
	if a.waitingToTakeOver() {
		t.Fatal("an engine-held row wrote a take-over request")
	}
	if a.at(pageHome) {
		t.Fatal("home stayed open over a conversation it had just opened")
	}
	if a.file != theirs {
		t.Fatalf("the surface landed on %q, want %q", a.file, theirs)
	}
}

// AND A ROW WITH NO ENGINE BEHIND IT KEEPS THE ASKING ROAD. A bare window holds
// the journal in its own process and the only thing anybody can do is ask it.
func TestEnterOnAWindowHeldRowStillArmsTheTakeover(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.engineAnswers = func(string) bool { return false }
	a.open = func(string, string) (Conversation, error) {
		t.Fatal("a window-held row went through the ordinary door")
		return Conversation{}, nil
	}
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))

	if a.home.armed != theirs {
		t.Fatalf("the row was not armed · armed=%q", a.home.armed)
	}
	if !strings.Contains(a.home.msg, "enter again to move it here") {
		t.Fatalf("the armed sentence never appeared:\n%s", a.home.msg)
	}
}

// A DOOR WITH NO ENGINE SEAM AT ALL IS THE OLD ROAD UNCHANGED — the in-process
// window, and every test that predates this seam.
func TestAWindowWithNoEngineSeamKeepsTheAskingRoad(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.engineAnswers = nil
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))

	if a.home.armed != theirs {
		t.Fatalf("the row was not armed · armed=%q", a.home.armed)
	}
}

// AN ENGINE THAT ANSWERS AND THEN REFUSES THE JOURNAL FALLS BACK TO ASKING. That
// is a bare window holding the lock in a workspace which also has an engine, and
// the refusal is read rather than guessed at.
func TestAnEngineThatCannotOpenTheRowFallsBackToAsking(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)
	theirs := lab.session("-tmp-alpha", "aaaa000000000002", "the other terminal", where, now.Add(-time.Hour))
	lab.hold(theirs)

	a := lab.app(mine)
	a.engineAnswers = func(string) bool { return true }
	a.open = func(string, string) (Conversation, error) { return Conversation{}, session.ErrSessionLocked }
	a.openHome()
	a.home.point(theirs)
	a.homeKey(key("enter"))

	if a.home.armed != theirs {
		t.Fatalf("a refused open did not fall back to asking · armed=%q", a.home.armed)
	}
	if !a.at(pageHome) {
		t.Fatal("a refused open closed home")
	}
}

// ── the window that is stepped back ─────────────────────────────────────────

// THE MOVED WINDOW DETACHES AND NEVER CLOSES. The engine is still running the
// turn; a close would tell it the conversation is over, which is the one thing
// this road exists to avoid.
func TestAMovedWindowDetachesRatherThanClosingTheConversation(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)

	a := lab.app(mine)
	held := &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.agent = held
	a.taskEvent(session.Event{Kind: session.EventMoved, Text: session.MovedWord})

	if held.closes > 0 {
		t.Fatalf("a moved window closed the conversation %d times", held.closes)
	}
	if held.detaches != 1 {
		t.Fatalf("a moved window detached %d times, want once", held.detaches)
	}
	if a.agent == Agent(held) {
		t.Fatal("a moved window is still sitting in the conversation that left")
	}
	if said := homeNotes(a); !strings.Contains(said, session.TakeoverWord) {
		t.Fatalf("the window said %q about stepping back", said)
	}
	if !strings.Contains(homeNotes(a), "enter on home brings it back") {
		t.Fatalf("the window never named the way back: %q", homeNotes(a))
	}
}

// AND IT LANDS ON HOME WITH THAT ROW UNDER THE CURSOR, which is what makes the
// move symmetrical: the way back is the same single keystroke.
func TestAMovedWindowLandsOnHomePointedAtTheConversationThatLeft(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)

	a := lab.app(mine)
	a.agent = &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.taskEvent(session.Event{Kind: session.EventMoved, Text: session.MovedWord})

	if !a.at(pageHome) {
		t.Fatal("a moved window did not land on home")
	}
	line, ok := a.home.focusedLine()
	if !ok || line.row.Transcript != mine {
		t.Fatalf("home is pointed at %q, want the conversation that left (%q)", line.row.Transcript, mine)
	}
	// AND HOME ITSELF SAYS WHERE IT WENT. The note in the transcript is the
	// record; this is the line the person is actually looking at.
	if !strings.Contains(a.home.msg, session.TakeoverWord) {
		t.Fatalf("home said %q about the conversation that left", a.home.msg)
	}
}

// AND THE ROW IS AIMED AT AGAIN ON THE BEAT, because home reads the disk on its
// own clock and the row is very often not on the list at the instant the
// conversation goes.
func TestAMovedWindowAimsAtTheRowOnceHomeHasIt(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	where := lab.project("-tmp-alpha")
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "this window", where, now)

	a := lab.app(mine)
	a.agent = &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}
	a.taskEvent(session.Event{Kind: session.EventMoved, Text: session.MovedWord})

	if a.movedFrom != mine {
		t.Fatalf("the window is aiming at %q, want the conversation that left", a.movedFrom)
	}
	// THE BEAT AIMS AGAIN, because each reading of the disk puts the cursor back
	// on this window's own conversation.
	a.home.cursor = len(a.home.lines) - 1
	a.pointMovedRow()
	line, ok := a.home.focusedLine()
	if !ok || line.row.Transcript != mine {
		t.Fatalf("the beat did not aim at the moved row · %+v", line.row.Transcript)
	}
	// AND A KEY ENDS IT. The person has the cursor back.
	a.homeKey(key("down"))
	if a.movedFrom != "" {
		t.Fatalf("a keystroke did not give the cursor back: %q", a.movedFrom)
	}
}
