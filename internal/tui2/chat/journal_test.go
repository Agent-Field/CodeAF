package chat

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The P0 harness: journal coverage versus screen coverage, over every order a
// turn can end in.
//
// These tests use the REAL store rather than the package's fake, because the
// fake's journal is a message counter and the real one is not: a message's
// sequence is a journal sequence shared with every other event the engine
// writes, so the poll's cheap question ("has the watermark moved") and its
// expensive one ("what messages came after mine") are answered by two different
// numbers that only sometimes move together. Every bug 13.2 describes lives in
// that gap, and a fake that closes it by construction cannot show any of them.

func openJournal(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

func newJournalApp(t *testing.T, graph *store.Store, commander Commander, events <-chan StreamEvent) *App {
	t.Helper()
	return New(Options{
		Backend:   graph,
		Commander: commander,
		Session:   testSession,
		Events:    events,
		Profile:   tokens.NoColor,
		PollEvery: 0,
	})
}

// send drives one draft through the composer's exact route: queue, drain, run
// the command off the render goroutine, fold the result back in.
func send(t *testing.T, app *App, text string) store.Message {
	t.Helper()
	app.pending = append(app.pending, text)
	cmd := app.drain(nil)
	if cmd == nil {
		t.Fatal("a submitted draft produced no command")
	}
	result, ok := cmd().(postResultMsg)
	if !ok {
		t.Fatalf("post produced %T, want a postResultMsg", cmd())
	}
	if result.err != nil {
		t.Fatalf("post failed: %v", result.err)
	}
	app.applyPost(result)
	return result.message
}

// reply journals a head reply the way internal/head does: the single door, the
// user's own session, the agent role, no node behind it.
func reply(t *testing.T, graph *store.Store, body string, parts ...store.MessagePart) store.Message {
	t.Helper()
	posted, err := thread.Post(graph, store.Message{
		SessionID: testSession, Role: store.RoleAgent, Body: body, Parts: parts,
	})
	if err != nil {
		t.Fatalf("post reply: %v", err)
	}
	return posted
}

// settle polls until the surface says the journal is quiet, which is what a
// window that has been left alone for a moment has done.
func settle(t *testing.T, app *App) {
	t.Helper()
	for i := 0; i < 20; i++ {
		before := app.journal
		reads := 0
		app.polling = true
		cmd := app.pollCmd()
		result := cmd().(pollResultMsg)
		app.applyPoll(result)
		app.polling = false
		reads++
		if result.quiet && app.journal == before {
			return
		}
	}
	t.Fatal("the poll never went quiet")
}

// assertJournalOnScreen is the P0's whole question, asked as an assertion: every
// row the store holds for this room is a block the transcript holds.
func assertJournalOnScreen(t *testing.T, graph *store.Store, app *App) {
	t.Helper()
	messages, err := graph.Messages(testSession, 0, 1000)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("the journal is empty; the test proved nothing")
	}
	for _, message := range messages {
		if _, ok := app.transcript.IndexOf(messageID(message.Seq)); !ok {
			t.Errorf("journal seq %d (%s %q) never reached the screen",
				message.Seq, message.Role, message.Body)
		}
	}
}

// -- the six orders a turn can end in ----------------------------------------

func TestAReplyThatLandsBeforeTheStreamTearsDownStillRenders(t *testing.T) {
	graph := openJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{}, nil)

	send(t, app, "what is the plan")
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"the plan is`})
	reply(t, graph, "the plan is to ship")
	settle(t, app)
	stream(app, StreamEvent{Kind: StreamFinished, Session: testSession})

	assertJournalOnScreen(t, graph, app)
	if out := frame(app); !strings.Contains(out, "the plan is to ship") {
		t.Fatalf("the durable reply is not on screen:\n%s", out)
	}
}

func TestAReplyThatLandsDuringTeardownStillRenders(t *testing.T) {
	graph := openJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{}, nil)

	send(t, app, "what is the plan")
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"the plan is`})
	stream(app, StreamEvent{Kind: StreamFinished, Session: testSession})
	reply(t, graph, "the plan is to ship")
	settle(t, app)

	assertJournalOnScreen(t, graph, app)
	if out := frame(app); !strings.Contains(out, "the plan is to ship") {
		t.Fatalf("the durable reply is not on screen:\n%s", out)
	}
	if app.turn.active {
		t.Fatal("the live turn outlived the reply it was previewing")
	}
}

func TestAReplyThatLandsAfterTheFeedClosedStillRenders(t *testing.T) {
	graph := openJournal(t)
	events := make(chan StreamEvent)
	app := newJournalApp(t, graph, &fakeCommander{}, events)

	send(t, app, "what is the plan")
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	app.Update(streamClosedMsg{})
	reply(t, graph, "the plan is to ship")
	settle(t, app)

	assertJournalOnScreen(t, graph, app)
	if out := frame(app); !strings.Contains(out, "the plan is to ship") {
		t.Fatalf("the durable reply is not on screen:\n%s", out)
	}
}

func TestAReplyWithNoStreamAtAllStillRenders(t *testing.T) {
	graph := openJournal(t)
	app := newJournalApp(t, graph, nil, nil)

	send(t, app, "what is the plan")
	reply(t, graph, "the plan is to ship")
	settle(t, app)

	assertJournalOnScreen(t, graph, app)
	if app.turn.active {
		t.Fatal("the turn never retired")
	}
}

func TestAStreamThatNeverJournalsAReplyLeavesTheWordsOnScreen(t *testing.T) {
	graph := openJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{}, nil)

	send(t, app, "what is the plan")
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"as far as I got`})
	stream(app, StreamEvent{Kind: StreamFailed, Session: testSession})
	settle(t, app)

	assertJournalOnScreen(t, graph, app)
	out := frame(app)
	if !strings.Contains(out, "as far as I got") {
		t.Fatalf("the words that did arrive vanished:\n%s", out)
	}
}

func TestAnInterruptedTurnRendersTheStoppedReply(t *testing.T) {
	graph := openJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{stopped: true}, nil)

	send(t, app, "what is the plan")
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"as far as I got`})
	app.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	reply(t, graph, "as far as I got — stopped",
		store.EndedMark(*store.InterruptedEnd()))
	settle(t, app)

	assertJournalOnScreen(t, graph, app)
	out := frame(app)
	if !strings.Contains(out, "as far as I got") {
		t.Fatalf("the stopped reply is missing:\n%s", out)
	}
	if app.turn.active {
		t.Fatal("the interrupted turn never retired")
	}
}

func TestTwoTurnsBackToBackBothRender(t *testing.T) {
	graph := openJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{}, nil)

	send(t, app, "first question")
	reply(t, graph, "first answer")
	settle(t, app)
	send(t, app, "second question")
	reply(t, graph, "second answer")
	settle(t, app)

	assertJournalOnScreen(t, graph, app)
	out := frame(app)
	for _, want := range []string{"first question", "first answer", "second question", "second answer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("frame is missing %q:\n%s", want, out)
		}
	}
}

// A second send while the first turn is still being answered must not blank the
// live region: the awaiting line is the only thing telling the reader the
// machine heard them.
func TestASecondSendWhileATurnIsLiveKeepsTheAwaitingLineOnScreen(t *testing.T) {
	graph := openJournal(t)
	app := newJournalApp(t, graph, &fakeCommander{}, nil)

	send(t, app, "first question")
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"half an`})
	send(t, app, "actually, second question")

	out := frame(app)
	if !strings.Contains(out, "thinking") && !strings.Contains(out, "replying") {
		t.Fatalf("the awaiting line vanished when a second draft was sent:\n%s", out)
	}
	if !strings.Contains(out, "half an") {
		t.Fatalf("the words already streamed vanished when a second draft was sent:\n%s", out)
	}
}

// The journal is one sequence shared by every event kind, so a room's messages
// are a sparse subset of it. A poll that reads a full page must come back for
// the rest instead of waiting for the next event to wake it.
func TestAThreadLongerThanOnePageReachesTheScreen(t *testing.T) {
	graph := openJournal(t)
	for i := 0; i < messagePage+20; i++ {
		reply(t, graph, "row")
	}
	app := newJournalApp(t, graph, nil, nil)
	settle(t, app)
	assertJournalOnScreen(t, graph, app)
}

// 13.2's P0, reproduced and locked.
//
// A room with a history longer than several pages: the reader sends, the
// awaiting line comes up, the head journals its answer — and before the fix the
// answer sat in the store forever while the poll answered "quiet", because the
// window had declared itself current as of the newest EVENT after reading only
// the oldest page of MESSAGES. The awaiting line spun above it with nothing
// left that could ever retire it.
func TestAReplyIntoARoomWithAHistoryStillRenders(t *testing.T) {
	graph := openJournal(t)
	for i := 0; i < 4*messagePage; i++ {
		reply(t, graph, "an older row")
	}
	app := newJournalApp(t, graph, &fakeCommander{}, nil)
	settle(t, app)

	send(t, app, "a fresh question")
	settle(t, app)
	if !app.turn.active {
		t.Fatal("the send did not open a turn to watch")
	}

	answer := reply(t, graph, "THE ANSWER")
	settle(t, app)

	if _, ok := app.transcript.IndexOf(messageID(answer.Seq)); !ok {
		t.Fatalf("the reply (seq %d) never reached the screen; current-through=%d read-through=%d",
			answer.Seq, app.journal, app.watermark)
	}
	if app.turn.active {
		t.Fatal("the awaiting line is still up over a reply that has landed")
	}
	if out := frame(app); !strings.Contains(out, "THE ANSWER") {
		t.Fatalf("the reply is not on screen:\n%s", out)
	}
	assertJournalOnScreen(t, graph, app)
}

// The claim a quiet poll makes — "the store holds nothing you have not seen" —
// must never be made by a window that has read messages past the journal
// position it recorded. This is the invariant the ungated trace line watches
// for in the field, asserted here so the field never has to.
func TestAQuietPollNeverClaimsToBeCurrentWhileRowsAreUnread(t *testing.T) {
	graph := openJournal(t)
	for i := 0; i < 3*messagePage; i++ {
		reply(t, graph, "row")
	}
	app := newJournalApp(t, graph, nil, nil)

	for i := 0; i < 40; i++ {
		app.polling = true
		result := app.pollCmd()().(pollResultMsg)
		app.applyPoll(result)
		app.polling = false
		if result.quiet && app.behind() {
			t.Fatalf("a quiet poll claimed current-through=%d while read-through=%d",
				app.journal, app.watermark)
		}
		if result.quiet {
			break
		}
	}
	assertJournalOnScreen(t, graph, app)
	if app.behind() {
		t.Fatalf("the drain never finished: current-through=%d read-through=%d",
			app.journal, app.watermark)
	}
}

// The drain must not become a spin: a caught-up window goes back on the
// cadence, and a behind one comes straight back exactly until it is not.
func TestTheDrainStopsAtTheEndOfTheRoom(t *testing.T) {
	graph := openJournal(t)
	for i := 0; i < 2*messagePage; i++ {
		reply(t, graph, "row")
	}
	app := newJournalApp(t, graph, nil, nil)
	settle(t, app)

	before := app.journal
	app.polling = true
	result := app.pollCmd()().(pollResultMsg)
	app.applyPoll(result)
	if !result.quiet {
		t.Fatalf("a caught-up window read the thread again: page=%d", len(result.messages))
	}
	if app.behind() {
		t.Fatal("a caught-up window still reports itself behind")
	}
	if app.journal != before {
		t.Fatalf("a quiet poll moved the journal claim from %d to %d", before, app.journal)
	}
}
