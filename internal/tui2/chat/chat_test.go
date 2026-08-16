package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/keychip"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The assembly is tested the way it is built: through the seams. A fake store
// and a fake commander stand in for the engine, the app is driven by the same
// messages Bubble Tea would deliver, and every assertion is made against the
// frame the shell would put on the wire — because the laws being checked here
// (a cut turn renders cut, esc does what the awaiting line says, another room's
// tokens never bleed in) are laws about what a reader sees.

type fakeBackend struct {
	journal  int64
	messages []store.Message
	posted   []store.Message
	reads    int
	seq      int64

	room, turn         store.RoomSpend
	haveRoom, haveTurn bool
	spendErr           error
}

func (f *fakeBackend) LatestEventSeq() (int64, error) { return f.journal, nil }

func (f *fakeBackend) Messages(session string, after int64, _ int) ([]store.Message, error) {
	f.reads++
	var out []store.Message
	for _, message := range f.messages {
		if message.SessionID == session && message.Seq > after {
			out = append(out, message)
		}
	}
	return out, nil
}

func (f *fakeBackend) PostMessage(message store.Message) (store.Message, error) {
	f.seq++
	message.Seq = f.seq
	message.Time = time.Unix(0, 0)
	f.messages = append(f.messages, message)
	f.posted = append(f.posted, message)
	f.journal++
	return message, nil
}

// The two money windows (10.5.23). The default fake has never billed anything,
// which is the state the missing-data law renders as — — so every existing
// assertion about the strips keeps meaning what it meant. A test that wants
// numbers sets room/turn and flips the presence bits, which is the only way to
// get a figure on screen: presence, recorded-ness and a known window are three
// separate facts and none of them defaults to true.
func (f *fakeBackend) SessionSpend(string) (store.RoomSpend, bool, error) {
	return f.room, f.haveRoom, f.spendErr
}

func (f *fakeBackend) TurnSpend(string) (store.RoomSpend, bool, error) {
	return f.turn, f.haveTurn, f.spendErr
}

// add journals a message the way the engine would, and moves the watermark the
// poll reads.
func (f *fakeBackend) add(message store.Message) store.Message {
	f.seq++
	message.Seq = f.seq
	message.Time = time.Unix(0, 0)
	f.messages = append(f.messages, message)
	f.journal++
	return message
}

type fakeCommander struct {
	interrupted []string
	stopped     bool
	model       string
	// window is the context denominator. Zero is a model the catalog cannot
	// speak for, which draws no gauge — the honest default, since inventing a
	// window would put a percentage of nothing on screen.
	window int
}

func (f *fakeCommander) Interrupt(partial string) bool {
	f.interrupted = append(f.interrupted, partial)
	return f.stopped
}

func (f *fakeCommander) CurrentModel(string) string { return f.model }

func (f *fakeCommander) ContextWindow(string) (int, bool) { return f.window, f.window > 0 }

const testSession = "session-one"

func newTestApp(backend Backend, commander Commander, events <-chan StreamEvent) *App {
	return New(Options{
		Backend:   backend,
		Commander: commander,
		Session:   testSession,
		Events:    events,
		// NoColor keeps the frames assertable as text: every law under test is
		// about what the row SAYS, and an escape sequence in the middle of the
		// haystack proves nothing either way.
		Profile:   tokens.NoColor,
		Now:       fixedNow,
		PollEvery: time.Millisecond,
		// A ground and a home the frame can be compared against on any
		// machine: without them the place line would render whichever
		// directory the test binary happened to be run from.
		Root: "/home/someone/aforge-v2",
		Home: "/home/someone",
	})
}

// poll drives one complete store read into the app, synchronously.
func poll(t *testing.T, app *App) {
	t.Helper()
	app.polling = true
	cmd := app.pollCmd()
	if cmd == nil {
		t.Fatal("no poll command")
	}
	result, ok := cmd().(pollResultMsg)
	if !ok {
		t.Fatal("poll did not answer with a result")
	}
	app.applyPoll(result)
	app.polling = false
}

func frame(app *App) string { return app.Frame(80, 20) }

func TestPollRendersTheThreadAsBlocks(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "hello there"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "hello yourself"})
	// A message belonging to another room must never reach this transcript.
	backend.add(store.Message{SessionID: "elsewhere", Role: store.RoleAgent, Body: "not for you"})

	app := newTestApp(backend, nil, nil)
	poll(t, app)

	out := frame(app)
	for _, want := range []string{"you", "hello there", "aforge", "hello yourself"} {
		if !strings.Contains(out, want) {
			t.Fatalf("frame is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "not for you") {
		t.Fatalf("another room's message reached this transcript:\n%s", out)
	}
}

func TestPollStaysQuietWhileTheJournalHasNotMoved(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "one"})

	app := newTestApp(backend, nil, nil)
	poll(t, app)
	if backend.reads != 1 {
		t.Fatalf("first poll read the thread %d times, want 1", backend.reads)
	}
	poll(t, app)
	if backend.reads != 1 {
		t.Fatalf("a quiet poll read the thread again (%d reads); the watermark is the whole point",
			backend.reads)
	}
}

// The truncation law (12.5.2): a turn ended by anything other than its own
// completion renders visibly cut, and says how it ended.
func TestTruncatedTurnRendersVisiblyCut(t *testing.T) {
	cases := []struct {
		name string
		how  store.EndKind
		want string
	}{
		{"output cap", store.EndLength, "cut off — output cap"},
		{"stream dropped", store.EndStreamDrop, "cut off — stream dropped"},
		{"the user stopped it", store.EndInterrupted, "stopped by you"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			backend := &fakeBackend{}
			backend.add(store.Message{
				SessionID: testSession, Role: store.RoleAgent,
				Body:  "half an answer",
				Parts: []store.MessagePart{store.EndedMark(store.EndedPart{How: testCase.how})},
			})
			app := newTestApp(backend, nil, nil)
			poll(t, app)

			out := frame(app)
			if !strings.Contains(out, "half an answer") {
				t.Fatalf("the words that did arrive are missing:\n%s", out)
			}
			if !strings.Contains(out, testCase.want) {
				t.Fatalf("frame does not say how the turn ended (want %q):\n%s", testCase.want, out)
			}
		})
	}
}

func TestCompletedTurnCarriesNoMark(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "a whole answer"})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	if out := frame(app); strings.Contains(out, "cut off") {
		t.Fatalf("an ordinary turn was marked as cut:\n%s", out)
	}
}

// The artifact law (12.5.1): a deliverable is referenced as a thing on disk,
// never carried as prose.
func TestArtifactPartRendersAsAReference(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleAgent, Body: "wrote the diagram",
		Parts: []store.MessagePart{store.ArtifactRef(store.ArtifactPart{
			Path: "workspace/task-2/architecture.svg", Bytes: 1611,
		})},
	})
	app := newTestApp(backend, nil, nil)
	poll(t, app)

	out := frame(app)
	if !strings.Contains(out, "architecture.svg") {
		t.Fatalf("the artifact is not referenced by path:\n%s", out)
	}
	if !strings.Contains(out, tokens.GlyphCollapsed) {
		t.Fatalf("the artifact reference is not drawn as a reference row:\n%s", out)
	}
}

func TestStreamDeltasOnlyRenderForThisSession(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: "elsewhere"})
	if app.turn.active {
		t.Fatal("another room's turn became this room's turn")
	}

	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: "elsewhere",
		Delta: `{"reply":"other room words`})
	if out := frame(app); strings.Contains(out, "other room words") {
		t.Fatalf("another room's tokens bled into this transcript:\n%s", out)
	}

	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession,
		Delta: `{"reply":"these words are mine`})
	if out := frame(app); !strings.Contains(out, "these words are mine") {
		t.Fatalf("this room's tokens did not reach the live region:\n%s", out)
	}
}

func TestStreamedReplyGrowsByItsTail(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"one`})
	first := app.turn.reply
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: ` two three`})

	if app.turn.reply != first {
		t.Fatal("an appending stream replaced its block instead of growing it")
	}
	if got := app.turn.reply.Body(); got != "one two three" {
		t.Fatalf("streamed body = %q, want %q", got, "one two three")
	}
	if out := frame(app); !strings.Contains(out, "one two three") {
		t.Fatalf("the streamed reply is not on screen:\n%s", out)
	}
}

// escHint is what the awaiting line writes when esc would in fact interrupt.
//
// It is named here because the words on the BAR row are a verb·key chip that
// reads `interrupt esc` (§16's grammar, verb first), and a frame-wide substring
// search for the two words in either order would find that chip and pass on a
// frame where the awaiting line said nothing at all. The awaiting line's own
// wording is the one under test.
var escHint = tokens.GlyphSeparator + " " + keychip.Text(registry.ChipFor("interrupt", "esc"))

// 5.20 rule 6 and 8.2.21: the hint appears only when esc would in fact
// interrupt, and whatever the awaiting line says is what esc will do.
func TestAwaitingLineAdvertisesInterruptOnlyWhenEscWouldInterrupt(t *testing.T) {
	withHead := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	stream(withHead, StreamEvent{Kind: StreamStarted, Session: testSession})
	if out := frame(withHead); !strings.Contains(out, escHint) {
		t.Fatalf("a live turn does not advertise the interrupt:\n%s", out)
	}

	// A window with no head behind it cannot stop anything, so it must not say
	// it can.
	headless := newTestApp(&fakeBackend{}, nil, nil)
	stream(headless, StreamEvent{Kind: StreamStarted, Session: testSession})
	if out := frame(headless); strings.Contains(out, escHint) {
		t.Fatalf("a window with no commander advertised an interrupt it cannot perform:\n%s", out)
	}

	// Once the provider is done there is nothing left to interrupt either.
	stream(withHead, StreamEvent{Kind: StreamFinished, Session: testSession})
	if out := frame(withHead); strings.Contains(out, escHint) {
		t.Fatalf("a settled turn still advertised an interrupt:\n%s", out)
	}
}

func TestSettledWindowShowsNoAwaitingLine(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	if out := frame(app); strings.Contains(out, "thinking") {
		t.Fatalf("an idle window is pretending to wait:\n%s", out)
	}
}

func TestEscInterruptsTheTurnYouAreWatching(t *testing.T) {
	commander := &fakeCommander{stopped: true}
	app := newTestApp(&fakeBackend{}, commander, nil)
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession,
		Delta: `{"reply":"as far as I got`})

	app.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(commander.interrupted) != 1 {
		t.Fatalf("esc did not interrupt the turn (%d calls)", len(commander.interrupted))
	}
	if got := commander.interrupted[0]; got != "as far as I got" {
		t.Fatalf("interrupt carried %q, want the words already on screen", got)
	}
	if out := frame(app); strings.Contains(out, escHint) {
		t.Fatalf("the hint survived the interrupt it advertised:\n%s", out)
	}
}

func TestEscDoesNotInterruptWhenNothingIsStreaming(t *testing.T) {
	commander := &fakeCommander{}
	app := newTestApp(&fakeBackend{}, commander, nil)
	app.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(commander.interrupted) != 0 {
		t.Fatal("esc interrupted a turn that was not running")
	}
}

func TestEscNeverDestroysADraft(t *testing.T) {
	commander := &fakeCommander{}
	backend := &fakeBackend{}
	app := newTestApp(backend, commander, nil)

	for _, r := range "keep me" {
		app.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(backend.posted) != 0 {
		t.Fatal("esc submitted the draft")
	}
	if len(commander.interrupted) != 0 {
		t.Fatal("esc with a draft and no live turn reached the interrupt door")
	}
	// The words are stashed, not destroyed: the recall walk brings them back.
	app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if out := frame(app); !strings.Contains(out, "keep me") {
		t.Fatalf("the stashed draft is unreachable:\n%s", out)
	}
}

func TestSubmitPostsThroughTheStoreAndOpensATurn(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)

	app.pending = append(app.pending, "what is the plan")
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
	if len(backend.posted) != 1 || backend.posted[0].Body != "what is the plan" {
		t.Fatalf("the draft did not reach the store: %+v", backend.posted)
	}
	if backend.posted[0].SessionID != testSession {
		t.Fatalf("the turn was posted to %q, want %q", backend.posted[0].SessionID, testSession)
	}

	app.applyPost(result)
	if !app.turn.active {
		t.Fatal("posting did not open a turn to watch")
	}
	out := frame(app)
	if !strings.Contains(out, "what is the plan") {
		t.Fatalf("the send had no durable echo (5.20 rule 4):\n%s", out)
	}
	if !strings.Contains(out, "thinking") {
		t.Fatalf("the awaiting line is missing:\n%s", out)
	}
}

func TestDurableReplyRetiresTheLivePreview(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)

	user := backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "ping"})
	poll(t, app)
	app.beginTurn(user.Seq)
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"po`})
	if out := frame(app); !strings.Contains(out, "po") {
		t.Fatalf("the preview never drew:\n%s", out)
	}

	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "pong"})
	poll(t, app)

	if app.turn.active {
		t.Fatal("the live turn outlived the reply it was previewing")
	}
	out := frame(app)
	if !strings.Contains(out, "pong") {
		t.Fatalf("the durable reply is missing:\n%s", out)
	}
	if strings.Contains(out, "thinking") {
		t.Fatalf("the awaiting line outlived the turn:\n%s", out)
	}
	if _, live := app.transcript.IndexOf("live-reply"); live {
		t.Fatal("the preview block is still in the transcript")
	}
}

// A node's report is not the head's reply and must not retire the turn.
func TestANodeReportDoesNotRetireTheTurn(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)
	app.beginTurn(0)
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleAgent, NodeID: "task-1", Body: "job finished",
	})
	poll(t, app)
	if !app.turn.active {
		t.Fatal("a job's report retired a conversation the head is still having")
	}
	if out := frame(app); !strings.Contains(out, "job finished") {
		t.Fatalf("the job's report is missing:\n%s", out)
	}
}

func TestScrollKeysReachTheTranscriptAndNotTheComposer(t *testing.T) {
	backend := &fakeBackend{}
	for i := 0; i < 60; i++ {
		backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "line"})
	}
	app := newTestApp(backend, nil, nil)
	poll(t, app)
	frame(app)

	bottom := app.transcript.YOffset()
	app.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	frame(app)
	if app.transcript.YOffset() >= bottom {
		t.Fatalf("pgup did not scroll (offset %d, was %d)", app.transcript.YOffset(), bottom)
	}
	if out := frame(app); strings.Contains(out, "pgup") {
		t.Fatalf("a scroll key was typed into the composer:\n%s", out)
	}
}

// Esc with no draft and no live turn returns the reader to the live edge and
// destroys nothing.
func TestEscFromAnEmptyDraftReturnsToTheLiveEdge(t *testing.T) {
	backend := &fakeBackend{}
	for i := 0; i < 60; i++ {
		backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "line"})
	}
	app := newTestApp(backend, nil, nil)
	poll(t, app)
	frame(app)

	app.transcript.PageUp()
	frame(app)
	if app.transcript.AtBottom() {
		t.Fatal("the test could not scroll away from the live edge")
	}
	app.navigate()
	frame(app)
	if !app.transcript.AtBottom() {
		t.Fatal("esc did not return the reader to the live edge")
	}
}

// 10.2.6/7: model and tool text passes the sanitizer, and the v2 surface
// re-slots the classic sixteen onto the token layer's table.
func TestSanitizeUsesTheV2Palette(t *testing.T) {
	got := sanitizeText("\x1b[31mred\x1b[0m")
	if !strings.Contains(got, "[91m") {
		t.Fatalf("standard red was not remapped into the bright family: %q", got)
	}
	if strings.Contains(got, "[31m") {
		t.Fatalf("the original slot survived the chokepoint: %q", got)
	}
	if !strings.Contains(got, "red") {
		t.Fatalf("the text did not survive sanitizing: %q", got)
	}
}

func TestSanitizeReachesMessageBodiesAndTextParts(t *testing.T) {
	message := store.Message{
		Body:  "\x1b[31mbody\x1b[0m",
		Parts: []store.MessagePart{store.TextPart("\x1b[31mpart\x1b[0m")},
	}
	sanitizeMessage(&message)
	if strings.Contains(message.Body, "[31m") {
		t.Fatalf("the body skipped the chokepoint: %q", message.Body)
	}
	if strings.Contains(message.Parts[0].Text, "[31m") {
		t.Fatalf("a text part skipped the chokepoint: %q", message.Parts[0].Text)
	}
}

func TestPartialReplyDecodesAGrowingStream(t *testing.T) {
	cases := []struct {
		raw   string
		want  string
		found bool
	}{
		{`{"class":"answer"`, "", false},
		{`{"reply":"`, "", true},
		{`{"reply":"hi`, "hi", true},
		{`{"reply":"line\nbreak`, "line\nbreak", true},
		{`{"reply":"quote\"`, `quote"`, true},
		{`{"reply":"escape\`, "escape", true},
		{`{"reply":"done"}`, "done", true},
	}
	for _, testCase := range cases {
		got, found := partialReply(testCase.raw)
		if found != testCase.found || got != testCase.want {
			t.Fatalf("partialReply(%q) = %q,%v; want %q,%v",
				testCase.raw, got, found, testCase.want, testCase.found)
		}
	}
}

func TestFrameSurvivesEveryWidth(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "a reasonably long line of prose"})
	backend.add(store.Message{
		SessionID: testSession, Role: store.RoleAgent, Body: "and a reply that was cut short",
		Parts: []store.MessagePart{
			store.ArtifactRef(store.ArtifactPart{Path: "a/b/c/deliverable.svg", Bytes: 2048}),
			store.EndedMark(store.EndedPart{How: store.EndLength}),
		},
	})
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"streaming`})

	for width := 1; width <= 140; width++ {
		for _, height := range []int{1, 2, 5, 24} {
			out := app.Frame(width, height)
			for _, row := range strings.Split(out, "\n") {
				if got := len([]rune(row)); got > width*2 {
					t.Fatalf("row at width %d is %d runes: %q", width, got, row)
				}
			}
		}
	}
}

// stream drives events in through the same door Bubble Tea uses, so a test
// exercises the repaint gate rather than reaching around it.
func stream(app *App, events ...StreamEvent) {
	app.Update(streamBatchMsg{events: events})
}

// An interrupt is the reader's own decision, so the provider's verdict on the
// call they cancelled must not overwrite what the surface says about it (5.16:
// the user's esc key is never painted as a failure).
func TestAnInterruptOutranksTheProvidersVerdict(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{stopped: true}, nil)
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	app.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	stream(app, StreamEvent{Kind: StreamFailed, Session: testSession})

	out := frame(app)
	if strings.Contains(out, "stream lost") {
		t.Fatalf("a cancelled call was reported as a failure:\n%s", out)
	}
	if !strings.Contains(out, "stopping") {
		t.Fatalf("the awaiting line does not say the turn is being stopped:\n%s", out)
	}
}
