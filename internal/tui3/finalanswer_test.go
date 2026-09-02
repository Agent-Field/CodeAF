package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// WHAT A TURN'S END OWES THE REPLY IT WAS WRITING (#225).
//
// The report was a surface unsure what the final answer is: a reply that sat
// raw until the next question, and a reply drawn as though it were thinking.
// The half of it that lives here is the FIRST — the turn boundary, and what may
// happen to a block after it has gone by. The other half is in
// internal/provider's answer_test.go, because deciding which streamed words are
// the answer needs the wire and this surface never sees it.
//
// Two laws, and they are separate:
//
//  1. NOTHING STREAMED OUTLIVES THE SETTLE. After a turn ends nothing is live,
//     and a delta arriving after it lands SETTLED on the answer it belongs to —
//     never as a new live block for the next question to close ([feed.settledTurn],
//     [app.growSettledAnswer]).
//  2. A LINE THIS SURFACE WROTE DOES NOT CUT THE REPLY IN TWO. A note landing
//     mid-answer used to close the live block, so the rest of the reply opened a
//     second one and the first half was demoted into narration — which is drawn
//     plain, so half a markdown answer came back as its source characters
//     ([feed.noteWritten] through [feed.said], hierarchy.go).
//
// The wants are asked of the STRUCTURE and of the RENDERED ROWS both, for
// settleboundary_test.go's reason: a test that sniffed the text for markdown
// would be agreeing with the defect rather than catching it.

// lateAnswer is the reply the report is about — a heading, a bold lead and a
// table, so a block that missed the markdown pipe says so three ways at once.
const lateAnswer = "## What a semaphore is\n\n" +
	"**A counter.** It admits as many as it holds.\n\n" +
	"| primitive | owns |\n| --- | --- |\n| mutex | yes |\n"

// rawLateMarkdown reports that a rendered row is the source text rather than
// the rendering of it — the exact thing the report described.
func rawLateMarkdown(row string) bool {
	return strings.Contains(row, "## What a semaphore is") ||
		strings.Contains(row, "**A counter.**") ||
		strings.Contains(row, "| primitive | owns |")
}

// noEntryIsLive is the law stated on its own, so a failure names it.
func noEntryIsLive(t *testing.T, a *app) {
	t.Helper()
	if a.live >= 0 {
		t.Fatalf("entry %d is still live after the turn settled", a.live)
	}
	for i := range a.entries {
		if e := &a.entries[i]; e.kind == entryAssistant && !e.settled {
			t.Fatalf("assistant block %d (turn %d) never settled: %q", i, e.turn, e.text)
		}
	}
}

// A DELTA THAT ARRIVES AFTER THE TURN SAID IT WAS DONE goes onto the answer it
// belongs to, settled. The report's symptom was the opposite: it opened a block
// nothing owned, which drew raw until the next question closed it and demoted
// the real answer above it on the way past.
func TestADeltaAfterTheTurnEndsLandsSettledOnTheAnswer(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, lateAnswer),
		{Kind: session.EventTurnDone},
		text(session.EventTextDelta, "One more clause the provider had buffered."),
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "what is a semaphore?")

	noEntryIsLive(t, a)
	blocks := answerBlocks(a)
	if len(blocks) != 1 {
		t.Fatalf("the straggler opened a second block: %d assistant blocks, want 1", len(blocks))
	}
	if !strings.HasSuffix(blocks[0].text, "One more clause the provider had buffered.") {
		t.Fatalf("the late words went somewhere else: %q", blocks[0].text)
	}
	if blocks[0].demoted {
		t.Fatalf("the answer was classified as narration by the delta that followed it")
	}
	for _, row := range plainRows(a) {
		if rawLateMarkdown(row) {
			t.Fatalf("the answer drew its source text:\n%s", strings.Join(plainRows(a), "\n"))
		}
	}
}

// AND THE NEXT QUESTION SETTLES NOTHING, which is the report's own sentence
// turned into a want: what a person sees when the turn ends is what they see
// afterwards, and the surface is not allowed to need a second question to
// finish the first one.
func TestTheNextQuestionSettlesNothingTheLastTurnLeftBehind(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{
		{
			text(session.EventTextDelta, lateAnswer),
			{Kind: session.EventTurnDone},
			text(session.EventTextDelta, " And a trailing sentence."),
		},
		{text(session.EventTextDelta, "A mutex owns."), {Kind: session.EventTurnDone}},
	}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "what is a semaphore?")

	before := append([]string(nil), plainRows(a)...)
	runTurn(t, a, agent, "and a mutex?")
	after := plainRows(a)

	// The first turn's rows are the head of the second turn's rows. A row that
	// changed there is a row the next question repaired, which is the defect.
	if len(after) < len(before) {
		t.Fatalf("the second turn lost rows the first had drawn")
	}
	for i, row := range before {
		if after[i] != row {
			t.Fatalf("row %d changed when the next question was asked:\n before %q\n  after %q", i, row, after[i])
		}
	}
}

// THE STOP GUARD IS THE SAME BOUNDARY. Between a person's esc and the stream
// closing the engine is still winding down and the provider is still speaking;
// none of what arrives may leave a block for the next question to close.
func TestAStoppedTurnLeavesNothingLiveForTheNextQuestion(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, lateAnswer),
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	typeLine(t, a, "what is a semaphore?")
	drive(t, a, key("esc"))
	agent.live <- text(session.EventTextDelta, " a tail the provider had already sent.")
	agent.live <- session.Event{Kind: session.EventTurnDone}
	drive(t, a, streamEventMsg{gen: a.gen})
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	noEntryIsLive(t, a)
	if n := len(answerBlocks(a)); n != 1 {
		t.Fatalf("the stopped turn drew %d assistant blocks, want 1", n)
	}
}

// A NOTE DOES NOT CUT THE REPLY IN TWO. The surface's own line lands under the
// block that is streaming and the block goes on growing, so a reply that had a
// notice written through the middle of it is still ONE answer.
func TestANoteMidAnswerDoesNotBreakTheReplyInTwo(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, lateAnswer),
		{Kind: session.EventNotice, Text: "trimmed the request and asked again"},
		text(session.EventTextDelta, "And the rest of the same answer."),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "what is a semaphore?")

	noEntryIsLive(t, a)
	blocks := answerBlocks(a)
	if len(blocks) != 1 {
		t.Fatalf("the note broke the reply into %d blocks, want 1", len(blocks))
	}
	if blocks[0].demoted {
		t.Fatalf("the first half of the answer was classified as narration")
	}
	for _, row := range plainRows(a) {
		if rawLateMarkdown(row) {
			t.Fatalf("half an answer drew its source text:\n%s", strings.Join(plainRows(a), "\n"))
		}
	}
	// The note is still in the transcript, and still under the reply it is about.
	var sawNote bool
	for i := range a.entries {
		if a.entries[i].kind == entryNote {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("the note was lost")
	}
}

// AND A CALL STILL DEMOTES WHAT CAME BEFORE IT, which is the law this change
// must not weaken: prose followed by more work in the same turn was narration,
// and that is decided by the work and never by a line the surface wrote.
func TestWorkStillDemotesTheProseInFrontOfIt(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "let me read the file first."),
		toolBegin("read", "notes.md"),
		toolEnd("read", "ok"),
		text(session.EventTextDelta, lateAnswer),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	a.width, a.height = 80, 40
	runTurn(t, a, agent, "what is a semaphore?")

	noEntryIsLive(t, a)
	blocks := answerBlocks(a)
	if len(blocks) != 2 {
		t.Fatalf("the turn drew %d assistant blocks, want 2", len(blocks))
	}
	if !blocks[0].demoted {
		t.Fatalf("narration in front of a call was promoted to the answer")
	}
	if blocks[1].demoted {
		t.Fatalf("the answer after the call was demoted")
	}
}
