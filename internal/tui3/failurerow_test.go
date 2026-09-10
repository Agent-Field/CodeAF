package tui3

import (
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE FAILURE STORY, ON THE SCREEN.
//
// These are the owner's report of 2026-09-10 written down as tests: a turn ran
// two commands, failed the follow-up request four times over ninety seconds and
// gave up — and what was on the screen two minutes later was a step that still
// said `running 2 commands` with nothing under it. "Things are just stuck".
//
// Each of them asserts THE RENDERED FRAME rather than a field, because every one
// of the defects was a fact the surface held perfectly well and never drew.

// failureNotes is every line a surface wrote about itself, in order. It takes the
// entry list rather than the app, because the two surfaces under test here hold
// theirs in different places ([app.entries] and [taskRoom.entries]).
func failureNotes(es []entry) []string {
	var out []string
	for _, e := range es {
		if e.kind == entryNote {
			out = append(out, e.text)
		}
	}
	return out
}

// failingTurn is the measured shape: a batch of two bash calls that both come
// back, then a ladder of retries, then the ladder running out.
func failingTurn(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "moonshot/kimi-k3"})
	a.width, a.height = 100, 40
	a.state, a.turn, a.model = stateWorking, 1, "moonshot/kimi-k3"
	a.entries = append(a.entries, entry{kind: entryUser, text: "check the tree", turn: 1})
	for _, args := range []string{`{"command":"git status"}`, `{"command":"go build ./..."}`} {
		a.event(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: args, Args: args})
	}
	for _, args := range []string{`{"command":"git status"}`, `{"command":"go build ./..."}`} {
		a.event(session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: args, Args: args, Output: "ok"})
	}
	return a
}

// ── the step that would not stop claiming to be running ─────────────────────

// A BATCH WHOSE CALLS HAVE ALL COME BACK IS NOT RUNNING, and its title says so.
// This is the whole of defect 1: the floor caption is composed in the present
// because it is normally composed while the calls are going, and it was never
// re-spelled when they stopped.
func TestAFinishedBatchIsNotCaptionedAsRunning(t *testing.T) {
	a := failingTurn(t)
	a.touch()
	page := plain(strings.Join(plainRows(a), "\n"))
	if strings.Contains(page, "running 2 commands") {
		t.Fatalf("a batch with both calls closed is still captioned as running:\n%s", page)
	}
	if !strings.Contains(page, "ran 2 commands") {
		t.Fatalf("the finished batch never took the past tense:\n%s", page)
	}
}

// AND A BATCH THAT IS STILL OPEN KEEPS THE PRESENT, which is the other half of
// the same claim and the reason the tense means anything at all.
func TestAnOpenBatchKeepsThePresentTense(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state, a.turn = stateWorking, 1
	for _, args := range []string{`{"command":"git status"}`, `{"command":"go build ./..."}`} {
		a.event(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: args, Args: args})
	}
	// One of the two comes back; the other is still going.
	a.event(session.Event{Kind: session.EventToolEnd, Tool: "bash",
		CallID: `{"command":"git status"}`, Args: `{"command":"git status"}`, Output: "ok"})
	captions := deriveCaptions(a.entries, a.turn)
	if len(captions) != 1 {
		t.Fatalf("two parallel calls made %d steps", len(captions))
	}
	if got := captionText(captions[0]); got != "running 2 commands" {
		t.Fatalf("a batch with a call still open reads %q, want the present tense", got)
	}
}

// The tense table itself, on every shape the floor can compose. It is a table
// because [captionPastVerbs] is one, and a verb added to the floor with no past
// spelling beside it is exactly the kind of omission nobody notices.
func TestEveryComposedCaptionHasAPastTense(t *testing.T) {
	for _, c := range []struct{ now, past string }{
		{"running 2 commands", "ran 2 commands"},
		{"reading 3 files in internal/tui3", "read 3 files in internal/tui3"},
		{"searching the tree", "searched the tree"},
		{"editing 2 files and running the suite", "edited 2 files and ran the suite"},
		{"building", "built"},
		{"listing github issues", "listed github issues"},
		{"looking at pull requests", "looked at pull requests"},
		{"asking the github api", "asked the github api"},
		{"checking git status", "checked git status"},
		{"working in git", "worked in git"},
		{"fetching example.com", "fetched example.com"},
		// A raw command slice has no tense to change and is left alone.
		{"go vet ./...", "go vet ./..."},
	} {
		if got := captionPast(c.now); got != c.past {
			t.Errorf("captionPast(%q) = %q, want %q", c.now, got, c.past)
		}
	}
}

// ── the ninety seconds nothing was said about ───────────────────────────────

// THE MEASURED SEQUENCE, END TO END: two calls come back, three requests fail,
// the ladder runs out. Every one of those has to be readable afterwards by
// somebody who was not watching the status line at the time.
func TestARetriedTurnThatGivesUpSaysBothInTheConversation(t *testing.T) {
	a := failingTurn(t)
	for i := 0; i < 3; i++ {
		a.event(session.Event{Kind: session.EventRetrying, Text: "the request failed — asking again"})
	}
	a.event(session.Event{Kind: session.EventError,
		Err: errors.New("after 3 retries: API error (429) rate limited")})
	a.touch()
	page := plain(strings.Join(plainRows(a), "\n"))

	// The reason the requests were being asked again, where the person is reading.
	if !strings.Contains(page, "the request failed — asking again") {
		t.Fatalf("three retries left no row in the conversation:\n%s", page)
	}
	// AND THE END, in the words it is: the row that was missing entirely.
	if !strings.Contains(page, "gave up after 4 tries · API error (429) rate limited") {
		t.Fatalf("the turn gave up and the conversation does not say so:\n%s", page)
	}
	// The engine's own arithmetic is not drawn beside the surface's — one line
	// saying `after 3 retries` inside `gave up after 4 tries` is the machinery
	// leaking through the sentence written to replace it.
	if strings.Contains(page, "after 3 retries") {
		t.Fatalf("the engine's retry count is still on the row:\n%s", page)
	}
	// AND THE STEP ABOVE IT IS NOT CLAIMING TO RUN. The three assertions are one
	// screen: a step that says it is running, over a failure nobody drew, is the
	// screen the owner photographed.
	if strings.Contains(page, "running 2 commands") {
		t.Fatalf("the failed turn left a step captioned as running:\n%s", page)
	}
}

// AN ERROR NOBODY TRIED AGAIN FOR IS STILL AN ERROR. "Gave up" is a claim about
// a struggle and a turn that failed once had none.
func TestAnErrorWithNoRetriesKeepsThePlainLine(t *testing.T) {
	a := failingTurn(t)
	a.event(session.Event{Kind: session.EventError, Err: errors.New("the request is too large for this model")})
	notes := failureNotes(a.entries)
	if len(notes) != 1 || notes[0] != "error: the request is too large for this model" {
		t.Fatalf("a single failure did not draw the plain error line: %v", notes)
	}
}

// ── one struct, three places ────────────────────────────────────────────────

// THE SAME FAILURE READS THE SAME WHEREVER WORK IS DRAWN. The conversation and
// a node's page take the same events through the same reducer, and a row that
// differed between them would be two accounts of one moment — which is what the
// surface had before failurerow.go: the room copied the chat's spelling by hand.
func TestOneFailureReadsTheSameInTheConversationAndInARoom(t *testing.T) {
	events := []session.Event{
		{Kind: session.EventRetrying, Text: "the model went quiet mid-reply — asking again"},
		{Kind: session.EventError, Err: errors.New("after 3 retries: API error (429) rate limited")},
	}

	chat := newTestApp(&fakeAgent{})
	chat.state, chat.turn = stateWorking, 1
	for _, ev := range events {
		chat.event(ev)
	}

	room := newTestApp(&fakeAgent{})
	room.room = room.newRoom(7, "port the parser")
	room.room.turn = 1
	for _, ev := range events {
		room.roomEvent(ev)
	}

	said, drawn := failureNotes(chat.entries), failureNotes(room.room.entries)
	if len(said) != 2 {
		t.Fatalf("the conversation drew %d rows for a retry and a give-up: %v", len(said), said)
	}
	if strings.Join(said, "\n") != strings.Join(drawn, "\n") {
		t.Fatalf("one failure, two spellings:\nconversation %v\nroom         %v", said, drawn)
	}
}

// AND A NODE'S PAGE STOPS SAYING NOTHING HAS ARRIVED. A sizing that timed out
// twice left [roomYetWord] standing for three minutes and twenty seconds while
// the node's own record held the failures.
func TestANodesPageDrawsItsFailuresRatherThanTheEmptyWord(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.room = a.newRoom(7, "size the review")
	a.room.turn = 1
	for _, ev := range []session.Event{
		{Kind: session.EventRetrying, Text: "nothing came back from the model — asking again"},
		{Kind: session.EventError, Err: errors.New("after 1 retries: nobody answered in time")},
	} {
		a.roomEvent(ev)
	}
	if n := len(failureNotes(a.room.entries)); n != 2 {
		t.Fatalf("two failures drew %d rows: %v", n, failureNotes(a.room.entries))
	}
	page := plain(strings.Join(roomLines(a), "\n"))
	if strings.Contains(page, roomYetWord) {
		t.Fatalf("the page still says nothing has arrived over two failure rows:\n%s", page)
	}
	if !strings.Contains(page, "gave up after 2 tries · nobody answered in time") {
		t.Fatalf("the node's page never said it gave up:\n%s", page)
	}
}

// ── the composer's own shapes ───────────────────────────────────────────────

// The vocabulary table, asserted at the door every surface goes through.
func TestTheFailureRowSpellsEveryShape(t *testing.T) {
	for _, c := range []struct {
		name string
		f    failure
		want string
	}{{
		name: "a sentence the engine composed is drawn whole",
		f:    failure{told: "the reply was cut short — asking again"},
		want: "the reply was cut short — asking again",
	}, {
		name: "and gains the arithmetic when the engine sends it",
		f:    failure{told: "the reply was cut short — asking again", attempt: 2, attempts: 4},
		want: "the reply was cut short — asking again · 2 of 4",
	}, {
		name: "a bare reason gets this surface's own verb",
		f:    failure{reason: "nobody answered in time", attempt: 2, attempts: 2},
		want: "nobody answered in time · asking again · 2 of 2",
	}, {
		name: "a hop names where the rest of the answer comes from",
		f:    failure{reason: "nobody answered in time", next: "moonshot/kimi-k3"},
		want: "nobody answered in time · moving to kimi-k3",
	}, {
		name: "and a hop draws no arithmetic — the count belonged to the model it left",
		f:    failure{reason: "nobody answered in time", next: "moonshot/kimi-k3", attempt: 4, attempts: 4},
		want: "nobody answered in time · moving to kimi-k3",
	}, {
		name: "an ordinal with no total is not drawn — the emptiness law",
		f:    failure{reason: "nobody answered in time", attempt: 3},
		want: "nobody answered in time · asking again",
	}, {
		name: "the end of the ladder counts the tries",
		f:    failure{reason: "API error (429) rate limited", gaveUp: true, tries: 4},
		want: "gave up after 4 tries · API error (429) rate limited",
	}, {
		name: "a give-up nobody counted says only that it stopped",
		f:    failure{reason: "nobody answered in time", gaveUp: true, tries: 1},
		want: "gave up · nobody answered in time",
	}} {
		if got := failureRow(c.f); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// THE PARTS ARE PREFERRED OVER THE SENTENCE. The engine sends both on every
// retry ([session.RetryNews]); the row built from the parts is the one that can
// say the arithmetic and tell a hop from another try of the same model.
func TestARetryRowIsBuiltFromTheEventsPartsWhenItHasThem(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.state, a.turn = stateWorking, 1
	a.event(session.Event{
		Kind: session.EventRetrying,
		Text: "the model went quiet mid-reply — asking again",
		Retry: &session.RetryNews{
			Model: "moonshot/kimi-k3", Attempt: 2, Attempts: 4,
			Reason: "the model went quiet mid-reply",
		},
	})
	a.event(session.Event{
		Kind: session.EventRetrying,
		Text: "the model kept going quiet mid-reply — finishing this one on glm-5.3",
		Retry: &session.RetryNews{
			Model: "moonshot/kimi-k3", Attempt: 4, Attempts: 4,
			Reason: "the model kept going quiet mid-reply", Next: "z-ai/glm-5.3",
		},
	})
	want := []string{
		"the model went quiet mid-reply · asking again · 2 of 4",
		"the model kept going quiet mid-reply · moving to glm-5.3",
	}
	if got := failureNotes(a.entries); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the rows read\n%v\nwant\n%v", got, want)
	}
	// AND THE STATUS LINE IS THE SAME STRUCT, so the hop it shows is the hop the
	// row shows.
	if got := failureDetail(a.lastAsk); got != "moving to glm-5.3" {
		t.Fatalf("the status line detail reads %q", got)
	}
}

// AN ENGINE THAT SENDS NO PARTS still draws its sentence whole — an older build
// on the far end of a `--host` link, and the shape this surface had before the
// parts existed.
func TestARetryWithNoPartsDrawsTheEnginesSentenceWhole(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.state, a.turn = stateWorking, 1
	a.event(session.Event{Kind: session.EventRetrying, Text: "the reply was cut short — asking again"})
	notes := failureNotes(a.entries)
	if len(notes) != 1 || notes[0] != "the reply was cut short — asking again" {
		t.Fatalf("an engine with no parts drew %v", notes)
	}
}

// The status line reads the SAME STRUCT, which is what keeps one story in three
// places. It is empty today because the engine sends no arithmetic — and empty
// draws nothing rather than an empty slot.
func TestTheStatusLineDetailComesOffTheSameStruct(t *testing.T) {
	if got := failureDetail(failure{told: "the request failed — asking again"}); got != "" {
		t.Fatalf("a retry with no arithmetic put %q on the status line", got)
	}
	if got := failureDetail(failure{attempt: 2, attempts: 4}); got != "2 of 4" {
		t.Fatalf("the status line detail reads %q, want the arithmetic", got)
	}
	if got := failureDetail(failure{next: "moonshot/kimi-k3"}); got != "moving to kimi-k3" {
		t.Fatalf("the status line detail reads %q, want the hop", got)
	}
}
