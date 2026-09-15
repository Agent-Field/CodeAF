package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE LAW: A TURN THE ENGINE ANSWERED IS DRAWN.
//
// Whatever the engine had to do to get an answer — hop to another model when the
// one the conversation names has no machines left, carry a correction across the
// seam into a turn of its own — the words it wrote reach the screen. A turn that
// runs, costs money, lands in the transcript and draws nothing is the worst
// failure this surface has, because there is nothing on screen for the person to
// disbelieve: they asked, they waited, and the page is the page they were
// already looking at.
//
// Measured on 2026-09-10 22:39 against dev@c0b204151, where three questions in a
// row were answered into a screen that showed none of them.

// hopEvent is one attempt being replaced by a DIFFERENT model — the shape
// [session.RetryNews] exists to make tellable, with `Next` set
// (internal/session's retrynews.go).
func hopEvent(from, to string) session.Event {
	return session.Event{
		Kind: session.EventRetrying,
		Text: "nothing kept coming back from the model · moving to " + to,
		Retry: &session.RetryNews{
			Model: from, Next: to, Attempt: 1, Attempts: 3,
			Reason: "nothing kept coming back from the model",
		},
	}
}

// A TURN THAT HOPPED DRAWS THE ANSWER THE MODEL IT HOPPED TO WROTE. The row
// saying the step moved is news about the machinery; the reply under it is the
// turn, and the two are not alternatives.
func TestAHoppedTurnDrawsItsAnswer(t *testing.T) {
	agent := &fakeAgent{model: "meta/muse-spark-1.3-contributor", turns: [][]session.Event{{
		hopEvent("meta/muse-spark-1.3-contributor", "meta/muse-spark-1.1"),
		text(session.EventTextDelta, "Site is live locally at http://localhost:8001"),
		{Kind: session.EventTurnDone, Usage: session.Usage{Input: 26333, Output: 78, CacheRead: 117440}},
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "whats up")

	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "Site is live locally") {
		t.Fatalf("a hopped turn drew no answer:\n%s", screen)
	}
	// And the hop left its own row behind — folded with the rest of the turn's
	// machinery, which is where news about the machinery belongs.
	said := false
	for i := range a.entries {
		if a.entries[i].kind == entryNote && strings.Contains(a.entries[i].text, "moving to muse-spark-1.1") {
			said = true
		}
	}
	if !said {
		t.Fatalf("the hop itself was never said:\n%s", transcriptText(a))
	}
}

// AND THE CALLS IT MADE AFTER THE HOP ARE DRAWN TOO. The hop takes the dead
// attempt's rows off the page; everything the new attempt does is the turn.
func TestAHoppedTurnDrawsTheCallsItMadeAfterTheHop(t *testing.T) {
	agent := &fakeAgent{model: "meta/muse-spark-1.3-contributor", turns: [][]session.Event{{
		hopEvent("meta/muse-spark-1.3-contributor", "meta/muse-spark-1.1"),
		toolBegin("bash", "ls -lh site/"),
		toolEnd("bash", "contact.html index.html studio.html work.html"),
		text(session.EventTextDelta, "Site is live locally at http://localhost:8001"),
		{Kind: session.EventTurnDone, Usage: session.Usage{Input: 26333, Output: 78, CacheRead: 117440}},
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "whats up")

	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "Site is live locally") {
		t.Fatalf("a hopped turn drew no answer:\n%s", screen)
	}
	tools := 0
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("the call the hopped turn made left %d rows, want 1:\n%s", tools, screen)
	}
}

// ── the seam a correction crosses ───────────────────────────────────────────

// THE DEFECT, AS IT HAPPENED: a correction typed into a running answer falls
// through — the turn ended before a boundary came — so the engine lifts it onto
// its own follow-up queue and carries its stream across (internal/session's
// [Agent.liftSteersLocked]). That news reaches this surface through a lane of its
// own, and it reaches it LATE by construction: a fall-through is what happens
// AFTER a turn ends, and by the time the lane is read the next turn — the
// previous correction's own — has usually already begun.
//
// The lane used to be stamped with [app.gen], which counts TURNS. So the stamp
// was stale exactly when it mattered, the channel was drained and thrown away,
// and a whole turn the engine ran, paid for and wrote into the transcript drew
// nothing at all: no question row, no reply, nothing. [app.convGen] moves only
// when the conversation is replaced, which is the question the check means to
// ask.
//
// THE ORDERING IS FORCED RATHER THAN RACED. The lane's command is taken from the
// real door — so the stamp under test is the one the surface really writes — and
// it is run after another turn has moved [app.gen], which is the order the
// program loop reaches it in whenever two corrections go into one answer.
func TestACorrectionWhoseFallThroughArrivesLateStillDrawsItsTurn(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")

	// The correction's own stream, carrying its acceptance, the seam, and the
	// turn those words then start.
	lane := make(chan session.Event, 8)
	lane <- session.Event{Kind: session.EventSteerAccepted, Steer: &session.SteerNote{ID: 1, Words: "whats up"}}
	lane <- session.Event{Kind: session.EventSteerFellThrough, Steer: &session.SteerNote{ID: 1, Words: "whats up"}}
	lane <- text(session.EventTextDelta, "All four pages done, consistency pass finished.")
	lane <- session.Event{Kind: session.EventTurnDone}
	close(lane)
	// The stamp is taken HERE, by the door that really takes it.
	held := a.holdSteer(lane)

	// Meanwhile the answer that was streaming ends and an ordinary turn goes by,
	// which is all it takes to move the turn generation.
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	agent.turns = [][]session.Event{{text(session.EventTextDelta, "Another answer."), {Kind: session.EventTurnDone}}}
	agent.turn = 0
	runTurn(t, a, agent, "and the wedge?")

	// Only now is the lane read.
	drive(t, a, runCmd(held)...)
	drive(t, a, frameMsg{})

	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "whats up") {
		t.Fatalf("the fallen-through correction never became a question on the page:\n%s", screen)
	}
	if !strings.Contains(screen, "All four pages done") {
		t.Fatalf("the fallen-through correction's own turn drew no answer:\n%s", screen)
	}
}

// AND THE STAMP IS ABOUT THE CONVERSATION, WHICH IS WHAT THE CHECK MEANS. It
// stands still through ordinary turns and moves when the conversation on screen
// is replaced — the one case where a stream from the session that has been left
// must not start a turn in the one that took its place (detach.go).
func TestTheSteerLaneStampMovesWithTheConversationAndNotWithTheTurn(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{
		{text(session.EventTextDelta, "one"), {Kind: session.EventTurnDone}},
		{text(session.EventTextDelta, "two"), {Kind: session.EventTurnDone}},
	}}
	a := newTestApp(agent)
	stamp, turnGen := a.convGen, a.gen
	runTurn(t, a, agent, "first")
	runTurn(t, a, agent, "second")
	if a.gen == turnGen {
		t.Fatal("two turns did not move the turn generation, so this proves nothing")
	}
	if a.convGen != stamp {
		t.Fatalf("an ordinary turn moved the steer lane's stamp: %d → %d", stamp, a.convGen)
	}
	a.detachConversation()
	if a.convGen == stamp {
		t.Fatal("a replaced conversation left the steer lane's stamp standing")
	}
}
