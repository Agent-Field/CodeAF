package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/charmbracelet/x/ansi"
)

// The activity region: what the turn is doing, and the one row it becomes.
//
// The laws under test are laws about what a reader sees, so every assertion is
// made against the frame the shell would put on the wire — with one exception,
// the compressor, which is a pure function about words and is tested as one.

// beginTurnWith opens a live turn and runs a batch of boundaries through the
// same door Bubble Tea would deliver them at — [App.Update], which is where the
// re-render that follows a stream event lives. Calling applyStream directly
// folds the state in and leaves the frame cached, which is a test asserting
// against a frame nobody would ever see.
func beginTurnWith(app *App, events ...StreamEvent) {
	app.beginTurn(app.watermark)
	feedTurn(app, events...)
}

// feedTurn delivers one batch into a turn that is already live.
func feedTurn(app *App, events ...StreamEvent) {
	batch := make([]StreamEvent, 0, len(events))
	for _, event := range events {
		if event.Session == "" {
			event.Session = testSession
		}
		batch = append(batch, event)
	}
	app.Update(streamBatchMsg{events: batch})
}

func toolBegin(gloss string) StreamEvent {
	return StreamEvent{Kind: StreamToolBegin, Delta: gloss}
}

func toolEnd(hint string) StreamEvent {
	return StreamEvent{Kind: StreamToolEnd, Delta: hint}
}

// A CALL IN FLIGHT IS A ROW, and the row says what the call is rather than that
// something is happening. This is the whole defect: a turn that reads for a
// minute used to show one pulsing word for all of it.
func TestALiveTurnDrawsWhatItIsDoing(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "what's running?"})
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)

	beginTurnWith(app,
		StreamEvent{Kind: StreamStarted},
		toolBegin("looking at the work"),
		toolEnd("3 rows"),
		toolBegin("searching for «pricing»"),
	)

	out := ansi.Strip(frame(app))
	for _, want := range []string{"looking at the work", "3 rows", "searching for «pricing»"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the live region is missing %q:\n%s", want, out)
		}
	}
	// The settled call wears the settled mark and the one still running wears
	// the working one. Two different sentences, and told apart with no colour.
	if !strings.Contains(out, "✓ looking at the work") {
		t.Fatalf("a finished call did not settle:\n%s", out)
	}
	if !strings.Contains(out, "◐ searching for «pricing»") {
		t.Fatalf("a running call is not marked as running:\n%s", out)
	}
	// And the awaiting line is still the last word: the activity pins ABOVE it.
	activityAt := strings.Index(out, "searching for «pricing»")
	waitAt := strings.LastIndex(out, "thinking")
	if activityAt < 0 || waitAt < 0 || waitAt < activityAt {
		t.Fatalf("the activity did not pin above the awaiting line:\n%s", out)
	}
}

// A FAILED CALL SAYS SO, in a mark and a word rather than in a colour.
func TestAFailedCallSettlesAsAFailure(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	poll(t, app)
	beginTurnWith(app,
		StreamEvent{Kind: StreamStarted},
		toolBegin("opening «task-3»"),
		StreamEvent{Kind: StreamToolFailed, Delta: "there is no live work with that id"},
	)
	out := ansi.Strip(frame(app))
	if !strings.Contains(out, "✕ opening «task-3»") {
		t.Fatalf("a failed call is not marked failed:\n%s", out)
	}
	if !strings.Contains(out, "there is no live work with that id") {
		t.Fatalf("a failed call did not say why:\n%s", out)
	}
}

// AN ORPHAN END RENDERS NOTHING (13.2's attach-mid-turn case). A window opened
// onto a room the head is already reading in heard the ending and never the
// beginning, and it has no honest row to draw for it. Inventing one would be a
// surface reporting work it has no account of.
func TestAWindowThatMissedTheBeginningDrawsNoOrphanEnding(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	poll(t, app)
	app.beginTurn(app.watermark)

	if moved := app.applyStream(StreamEvent{Kind: StreamToolEnd,
		Delta: "3 rows", Session: testSession}); moved {
		t.Fatal("an ending with no beginning moved the screen")
	}
	app.refresh()
	if app.turn.activity.live() {
		t.Fatalf("an ending with no beginning minted a step: %+v", app.turn.activity)
	}
	out := ansi.Strip(frame(app))
	if strings.Contains(out, "3 rows") {
		t.Fatalf("an orphan ending was drawn anyway:\n%s", out)
	}

	// And the region recovers: the next real beginning opens a row of its own.
	feedTurn(app, toolBegin("looking at the work"))
	if !strings.Contains(ansi.Strip(frame(app)), "looking at the work") {
		t.Fatal("the region did not recover after an orphan ending")
	}
}

// ANOTHER ROOM'S ACTIVITY IS ANOTHER ROOM'S. The session filter guards these
// boundaries exactly as it guards the tokens, because it is the same filter.
func TestAnotherRoomsActivityNeverReachesThisTranscript(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	poll(t, app)
	app.beginTurn(app.watermark)
	feedTurn(app, StreamEvent{Kind: StreamToolBegin,
		Delta: "reading someone else's plan", Session: "elsewhere"})
	if strings.Contains(ansi.Strip(frame(app)), "someone else") {
		t.Fatalf("another room's activity reached this transcript:\n%s", frame(app))
	}
}

// A LONG TURN DOES NOT WALK OFF THE BOTTOM OF THE SCREEN. The belt allows
// sixteen calls; sixteen live rows above the composer is a log, not a
// transcript. What is kept is the newest few plus a count of what went past.
func TestALongRunOfCallsKeepsTheLiveRegionBounded(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	poll(t, app)
	app.beginTurn(app.watermark)
	for _, gloss := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		feedTurn(app, toolBegin("reading «"+gloss+"»"), toolEnd(""))
	}
	rows := app.turn.activity.Rows(80)
	if len(rows) != activityLiveRows+1 {
		t.Fatalf("seven calls drew %d rows, want %d and the count", len(rows), activityLiveRows+1)
	}
	joined := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "2 earlier") {
		t.Fatalf("the rows that scrolled off are not counted:\n%s", joined)
	}
	if strings.Contains(joined, "«one»") {
		t.Fatalf("the region grew past its bound:\n%s", joined)
	}
	if !strings.Contains(joined, "«seven»") {
		t.Fatalf("the newest call is not the one shown:\n%s", joined)
	}
}

// -- the collapse -------------------------------------------------------------

// WHEN THE REPLY LANDS THE ROWS BECOME ONE ROW, attached under it, and the live
// region is gone.
func TestTheActivityCollapsesUnderTheReplyThatEndsTheTurn(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "what's running?"})
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)

	beginTurnWith(app,
		StreamEvent{Kind: StreamStarted},
		toolBegin("looking at the work"), toolEnd("3 rows"),
		toolBegin("searching for «pricing»"), toolEnd("2 rows"),
		toolBegin("reading the plan for «task-9»"), toolEnd(""),
	)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "Three things are running."})
	poll(t, app)

	if app.turn.active {
		t.Fatal("the durable reply did not end the live turn")
	}
	out := ansi.Strip(frame(app))
	if !strings.Contains(out, "3 steps") {
		t.Fatalf("the turn's activity did not collapse into a count:\n%s", out)
	}
	// The rows themselves are gone, behind the fold.
	if strings.Contains(out, "searching for «pricing»") {
		t.Fatalf("the collapsed activity still draws its rows:\n%s", out)
	}
	// The summary hangs on the reply, not on a block of its own.
	last, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok || len(last.activity) != 3 {
		t.Fatalf("the activity did not attach to the reply: %+v", last)
	}
	rows := ansi.Strip(blockRows(t, app, app.transcript.Len()-1, 80))
	if !strings.Contains(rows, "Three things are running.") {
		t.Fatalf("the summary attached to the wrong block:\n%s", rows)
	}
}

// AND IT OPENS, through the same per-block disclosure every other row here uses
// (disclose.go). The reader's decision is keyed by block id, so it survives the
// re-renders a running room performs several times a second.
func TestTheCollapsedActivityOpensAndReplaysEveryStep(t *testing.T) {
	app, block := collapsedTurn(t)
	if !block.collapsible {
		t.Fatal("the collapse row is not a door")
	}
	shut := ansi.Strip(blockRows(t, app, app.transcript.Len()-1, 80))
	if !strings.Contains(shut, blocks.CollapsedMark) {
		t.Fatalf("the shut row wears no disclosure mark:\n%s", shut)
	}

	app.toggleFold(block)
	open := ansi.Strip(blockRows(t, app, app.transcript.Len()-1, 80))
	if !strings.Contains(open, blocks.ExpandedMark) {
		t.Fatalf("the opened row did not turn its mark over:\n%s", open)
	}
	for _, want := range []string{"looking at the work", "searching for «pricing»",
		"reading the plan for «task-9»"} {
		if !strings.Contains(open, want) {
			t.Fatalf("the replay is missing %q:\n%s", want, open)
		}
	}

	// The reader's decision is remembered by id, which is what makes it survive
	// a rebuild rather than a re-render.
	if open, chosen := app.folds[block.ID()]; !chosen || !open {
		t.Fatalf("the fold decision was not remembered: open=%v chosen=%v", open, chosen)
	}
	app.toggleFold(block)
	if shutAgain := ansi.Strip(blockRows(t, app, app.transcript.Len()-1, 80)); strings.Contains(
		shutAgain, "searching for «pricing»") {
		t.Fatalf("the row did not close again:\n%s", shutAgain)
	}
}

// THE POINTER FINDS THE DOOR — and finds it on the summary row and on every row
// of the opened list, which is §10's expand law in full.
func TestThePointerResolvesTheActivityRow(t *testing.T) {
	app, block := collapsedTurn(t)
	rows := block.Rows(80)
	door := -1
	for i, row := range rows {
		if strings.Contains(ansi.Strip(row), "3 steps") {
			door = i
			break
		}
	}
	if door < 0 {
		t.Fatalf("no summary row to point at:\n%s", strings.Join(rows, "\n"))
	}
	if !block.isFoldRow(door, 80) {
		t.Fatalf("row %d is the summary and is not a door", door)
	}
	if block.isFoldRow(door-1, 80) {
		t.Fatalf("the row above the summary is a door it should not be")
	}
	app.toggleFold(block)
	if !block.isFoldRow(door+1, 80) {
		t.Fatal("a row of the opened list does not close it again")
	}
}

// AN ORDINARY TURN COLLAPSES INTO NOTHING. A conversation with no tool calls in
// it is the common case and must not grow a row saying so.
func TestATurnWithNoToolCallsLeavesNoRowBehind(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "hello"})
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	beginTurnWith(app, StreamEvent{Kind: StreamStarted})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "hello yourself"})
	poll(t, app)

	last, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatalf("no reply block: %T", app.transcript.Block(app.transcript.Len()-1))
	}
	if len(last.activity) != 0 || last.collapsible {
		t.Fatalf("an ordinary turn grew an activity row: %+v", last.activity)
	}
	if strings.Contains(ansi.Strip(frame(app)), "steps") {
		t.Fatalf("an ordinary turn said something about steps:\n%s", frame(app))
	}
}

// LINEAR MODE PRINTS THE SUMMARY AND NOT THE DOOR (10.1.5). The accessible
// rendering is one column of plain rows; content behind an interaction is
// content that surface cannot deliver. The words are the same words.
func TestLinearModeStatesTheSummaryWithNoDoor(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "what's running?"})
	app := New(Options{
		Backend: backend, Commander: &fakeCommander{}, Session: testSession,
		Profile: 0, Now: fixedNow, PollEvery: 1, Linear: true,
		Root: "/home/someone/aforge-v2", Home: "/home/someone",
	})
	poll(t, app)
	beginTurnWith(app,
		StreamEvent{Kind: StreamStarted},
		toolBegin("looking at the work"), toolEnd("3 rows"),
		toolBegin("searching for «pricing»"), toolEnd(""),
	)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "Three things are running."})
	poll(t, app)

	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatalf("no reply block: %T", app.transcript.Block(app.transcript.Len()-1))
	}
	if block.collapsible {
		t.Fatal("the accessible rendering grew a fold")
	}
	rows := ansi.Strip(strings.Join(block.Rows(80), "\n"))
	if !strings.Contains(rows, "2 steps") {
		t.Fatalf("the plain summary is missing:\n%s", rows)
	}
	if strings.Contains(rows, blocks.CollapsedMark) {
		t.Fatalf("the plain summary drew a door onto nothing:\n%s", rows)
	}
}

// collapsedTurn is a room with one finished turn that made three calls.
func collapsedTurn(t *testing.T) (*App, *messageBlock) {
	t.Helper()
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "what's running?"})
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	beginTurnWith(app,
		StreamEvent{Kind: StreamStarted},
		toolBegin("looking at the work"), toolEnd("3 rows"),
		toolBegin("searching for «pricing»"), toolEnd("2 rows"),
		toolBegin("reading the plan for «task-9»"), toolEnd(""),
	)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "Three things are running."})
	poll(t, app)
	block, ok := app.transcript.Block(app.transcript.Len() - 1).(*messageBlock)
	if !ok {
		t.Fatalf("no reply block: %T", app.transcript.Block(app.transcript.Len()-1))
	}
	return app, block
}

// -- the compressor ------------------------------------------------------------

// THE COUNT IS EXACT AND THE PHRASES ARE A READING. That asymmetry is the
// honesty of the row: the number says how much happened, the clause says what
// kind of thing happened, in the order it first happened.
func TestTheCompressorSaysTheCountAndTheNotableActs(t *testing.T) {
	cases := []struct {
		name  string
		steps []activityStep
		want  string
	}{
		{
			name: "the sketch, exactly",
			steps: []activityStep{
				{gloss: "searching for «pricing»", done: true},
				{gloss: "searching for «margins»", done: true},
				{gloss: "reading the plan for «task-9»", done: true},
				{gloss: "putting work in hand: «redo the margins»", done: true},
				{gloss: "looking at the work", done: true},
			},
			want: "5 steps — searched twice, read the plan, put work in hand …",
		},
		{
			name:  "one call says one step and no count beside it",
			steps: []activityStep{{gloss: "looking at the work", done: true}},
			want:  "1 step — looked at the work",
		},
		{
			name: "three of a kind says how many",
			steps: []activityStep{
				{gloss: "reading «a.md»", done: true},
				{gloss: "reading «b.md»", done: true},
				{gloss: "reading «c.md»", done: true},
			},
			want: "3 steps — read 3 times",
		},
		{
			// §1b. "1 didn't land" named no failure. The step that failed knows
			// which one it was, and its own gloss is the whole of the answer.
			name: "a single failure names itself at the end",
			steps: []activityStep{
				{gloss: "looking at the work", done: true},
				{gloss: "opening «task-3»", done: true, failed: true},
			},
			want: "2 steps — looked at the work, opened · opening «task-3» didn't land",
		},
		{
			name: "several failures keep the count and name the first",
			steps: []activityStep{
				{gloss: "opening «task-3»", done: true, failed: true},
				{gloss: "reading «a.md»", done: true, failed: true},
			},
			want: "2 steps — opened, read · 2 didn't land, from opening «task-3»",
		},
		{
			// A window that attached mid-turn saw the end and never the begin, so
			// it has no gloss to name. The count is all it honestly knows.
			name: "a failure with no gloss falls back to the count",
			steps: []activityStep{
				{gloss: "looking at the work", done: true},
				{gloss: "", done: true, failed: true},
			},
			want: "2 steps — looked at the work · 1 didn't land",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := compressActivity(testCase.steps); got != testCase.want {
				t.Errorf("compressed to\n  %q\nwant\n  %q", got, testCase.want)
			}
		})
	}
	if got := compressActivity(nil); got != "" {
		t.Errorf("no steps compressed to %q, want nothing at all", got)
	}
}

// THE ACT IS THE GLOSS WITH ITS SUBJECT TAKEN OFF AND PUT IN THE PAST. It is
// derived from the head's own words rather than from a second table here, so
// what a reader watched scroll past and what they read afterwards are the same
// vocabulary.
func TestPastActTakesTheSubjectOffAndTheTenseBack(t *testing.T) {
	cases := map[string]string{
		"searching for «navctx»":                 "searched",
		"reading the plan for «task-1»":          "read the plan",
		"putting work in hand: «fix the leak»":   "put work in hand",
		"looking at the work":                    "looked at the work",
		"looking through the work for «finance»": "looked through the work",
		"running «git status»":                   "ran",
		"writing «architecture.svg»":             "wrote",
		"checking the spend":                     "checked the spend",
		// A verb the table has never heard of keeps its gerund, which is a
		// slightly awkward sentence and never a wrong one.
		"frobnicating «x»": "frobnicating",
		// §1b. The gloss the trail printed as "read what". Whatever the cut
		// leaves hanging comes off, however many words deep it goes.
		"reading what came back from «task-8»": "read what came back",
		"reading what":                         "read",
		"reading a file from «task-8»":         "read a file",
		"reading a":                            "read",
		"opening the":                          "opened",
	}
	for gloss, want := range cases {
		if got := pastAct(gloss); got != want {
			t.Errorf("pastAct(%q) = %q, want %q", gloss, got, want)
		}
	}
}

// §1b, as the property rather than the table: no act this vocabulary can
// produce ends on a word that was holding a place for a subject that is gone.
// A trail that says "read what" has printed the seam it was supposed to hide.
func TestNoActEndsOnAWordLeftHangingByTheCut(t *testing.T) {
	glosses := []string{
		"reading what came back from «task-8»",
		"reading a file from «task-8»",
		"reading the plan for «task-1»",
		"looking through the work for «finance»",
		"looking at «task-1»",
		"changing «task-1»",
		"reading the manual on «threads»",
		"putting work in hand: «fix the leak»",
		"searching for «pricing»",
		"writing «brief.md»",
		"opening «task-1»",
	}
	for _, gloss := range glosses {
		act := pastAct(gloss)
		if act == "" {
			t.Errorf("pastAct(%q) said nothing at all", gloss)
			continue
		}
		fields := strings.Fields(act)
		if activityDanglers[fields[len(fields)-1]] {
			t.Errorf("pastAct(%q) = %q, which ends on a word with nothing after it", gloss, act)
		}
	}
}
