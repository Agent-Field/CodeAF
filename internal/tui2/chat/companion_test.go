package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "charm.land/bubbletea/v2"
)

// The companion keys, spelled the way the head spells them. They are literals
// here on purpose: a test that built them from this package's own constants
// would pass on the day a suffix was renamed on one side only, which is exactly
// the drift cmd/aforge's pin exists to catch (TestCompanionStreamKeysMatchTheHead).
const (
	deliveredKey = testSession + "#delivered"
	receiptKey   = testSession + "#receipt"
	asideKey     = testSession + "#aside"
)

// THE LAW: model-authored text that will render visibly on the open surface
// renders as its deltas arrive. The delivery answer lands as an ordinary agent
// row in the room, so it streams there rather than appearing whole.
func TestTheDeliveryAnswerStreamsIntoTheRoomItPostsTo(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: deliveredKey})
	if !app.turn.active {
		t.Fatal("a delivery answer opened no live region — it will appear whole")
	}
	stream(app, StreamEvent{Kind: StreamDelta, Session: deliveredKey,
		Delta: `{"reply":"the benchmark came back at 4.1s`})

	// Before Finished, and on the app's own frame: this is the whole claim.
	out := frame(app)
	if !strings.Contains(out, "the benchmark came back at 4.1s") {
		t.Fatalf("the delivery answer's deltas did not reach the screen:\n%s", out)
	}
	stream(app, StreamEvent{Kind: StreamFinished, Session: deliveredKey})
}

// The receipt wake is the other companion whose text becomes a row.
func TestTheReceiptWakeStreamsIntoTheRoomItPostsTo(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: receiptKey})
	stream(app, StreamEvent{Kind: StreamDelta, Session: receiptKey,
		Delta: `{"reply":"it took the narrower reading`})

	if out := frame(app); !strings.Contains(out, "it took the narrower reading") {
		t.Fatalf("the receipt wake's deltas did not reach the screen:\n%s", out)
	}
}

// THE EXCLUSION. An aside journals a COLLAPSED STUB, so what a reader gets is a
// fold — and folds never stream. A surface that drew these deltas would be
// typing an aside's words into the main transcript, which is the bug the key was
// invented to prevent (8.2.9).
func TestAnAsideNeverStreamsBecauseItsRowIsAFold(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: asideKey})
	if app.turn.active {
		t.Fatal("an aside opened a live region — its answer is a fold, not a reply")
	}
	stream(app, StreamEvent{Kind: StreamDelta, Session: asideKey,
		Delta: `{"reply":"it picked that one because`})
	if out := frame(app); strings.Contains(out, "it picked that one because") {
		t.Fatalf("an aside typed itself into the main transcript:\n%s", out)
	}
}

// A companion suffix this build has never heard of is refused rather than
// guessed at, for the reason headStreamKind states about unknown boundaries.
func TestAnUnknownCompanionKeyIsRefusedRatherThanDrawn(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession + "#whatever"})
	if app.turn.active {
		t.Fatal("a companion key from the future claimed this room's live region")
	}
}

// Another room's companion is another room's, exactly as its own turn would be.
func TestACompanionOfAnotherRoomIsDropped(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: "elsewhere#delivered"})
	if app.turn.active {
		t.Fatal("another room's delivery answer streamed into this one")
	}
}

// CATCH-UP STAYS WHOLE. The same answer, generated while this window was showing
// a different room, arrives as a journaled row and renders complete — with no
// live region, no caret and no half-drawn tail left behind it.
func TestADeliveryAnswerGeneratedElsewhereArrivesWholeOnOpen(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)

	// Every delta of it lands while the window is pointed somewhere else.
	stream(app, StreamEvent{Kind: StreamStarted, Session: "elsewhere#delivered"})
	stream(app, StreamEvent{Kind: StreamDelta, Session: "elsewhere#delivered",
		Delta: `{"reply":"the benchmark came back at 4.1s`})
	stream(app, StreamEvent{Kind: StreamFinished, Session: "elsewhere#delivered"})

	// Then the reader opens the room the answer was posted in.
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "the benchmark came back at 4.1s"})
	poll(t, app)

	out := frame(app)
	if !strings.Contains(out, "the benchmark came back at 4.1s") {
		t.Fatalf("the journaled answer is not on screen whole:\n%s", out)
	}
	if app.turn.active {
		t.Fatal("a live region survived a row that was never streamed here")
	}
}

// ONE LIVE REGION, ONE OWNER. A companion may not interleave its bytes with the
// reply a person is waiting on; it loses the region and its text arrives whole.
func TestACompanionNeverInterleavesWithTheRoomsOwnTurn(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession,
		Delta: `{"reply":"what you asked about is`})
	stream(app, StreamEvent{Kind: StreamStarted, Session: deliveredKey})
	stream(app, StreamEvent{Kind: StreamDelta, Session: deliveredKey,
		Delta: ` a completely different sentence`})

	out := frame(app)
	if strings.Contains(out, "a completely different sentence") {
		t.Fatalf("a companion's bytes were folded into the room's own reply:\n%s", out)
	}
	if !strings.Contains(out, "what you asked about is") {
		t.Fatalf("the room's own reply lost its words to a companion:\n%s", out)
	}
	if app.turn.companion != "" {
		t.Fatalf("the live region changed hands to %q while the room's own turn held it",
			app.turn.companion)
	}
}

// The other ordering: the room's own turn takes the region back, and the
// companion's half-drawn words go with it rather than being grown past.
func TestTheRoomsOwnTurnTakesTheLiveRegionBackFromACompanion(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: deliveredKey})
	stream(app, StreamEvent{Kind: StreamDelta, Session: deliveredKey,
		Delta: `{"reply":"about that job you asked for`})
	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession,
		Delta: `{"reply":"yes — here is what I think`})

	out := frame(app)
	if strings.Contains(out, "about that job you asked for") {
		t.Fatalf("the companion's words survived under the room's own reply:\n%s", out)
	}
	if !strings.Contains(out, "yes — here is what I think") {
		t.Fatalf("the room's own reply did not take the live region:\n%s", out)
	}
}

// A stray delta whose opening this window never saw mints nothing: what it would
// draw is a reply beginning in the middle.
func TestACompanionDeltaWithoutItsOpeningDrawsNothing(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)

	stream(app, StreamEvent{Kind: StreamDelta, Session: deliveredKey,
		Delta: `{"reply":"the middle of a sentence`})
	if app.turn.active {
		t.Fatal("a headless delta opened a live region")
	}
	if out := frame(app); strings.Contains(out, "the middle of a sentence") {
		t.Fatalf("a reply beginning in the middle reached the screen:\n%s", out)
	}
}

// INTERRUPT SURVIVES. esc is the innermost live scope only where it can in fact
// stop something. Head.Interrupt cancels the CONVERSATION's turn and nothing
// else, so a companion's live region must not advertise or claim the key — and
// the draft ring, which is the next rung down, must still get it.
func TestEscDoesNotClaimACompanionStreamAndTheDraftRingKeepsIt(t *testing.T) {
	commander := &fakeCommander{stopped: true}
	app := newTestApp(&fakeBackend{}, commander, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: deliveredKey})
	stream(app, StreamEvent{Kind: StreamDelta, Session: deliveredKey,
		Delta: `{"reply":"the benchmark came back`})
	if out := frame(app); strings.Contains(out, escHint) {
		t.Fatalf("a companion turn advertised an interrupt it cannot perform:\n%s", out)
	}
	if app.canInterrupt() {
		t.Fatal("esc claimed a turn its handle does not reach")
	}

	// The draft the reader was typing is what esc reaches instead, and it is
	// stashed rather than destroyed — which is the ring, checked by walking it.
	typeInto(app, "a sentence in progress")
	app.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(commander.interrupted) != 0 {
		t.Fatalf("esc interrupted the head over a companion turn (%d calls)",
			len(commander.interrupted))
	}
	if got := app.composer.Draft(); got != "" {
		t.Fatalf("esc did not reach the draft: the composer still holds %q", got)
	}
	app.key(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := app.composer.Draft(); got != "a sentence in progress" {
		t.Fatalf("the stashed draft did not come back off the ring: %q", got)
	}
}

// The room's own turn keeps every bit of the interrupt behaviour it had.
func TestEscStillInterruptsTheRoomsOwnTurn(t *testing.T) {
	commander := &fakeCommander{stopped: true}
	app := newTestApp(&fakeBackend{}, commander, nil)

	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	stream(app, StreamEvent{Kind: StreamDelta, Session: testSession,
		Delta: `{"reply":"as far as I got`})
	app.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(commander.interrupted) != 1 {
		t.Fatalf("esc stopped claiming the room's own turn (%d calls)",
			len(commander.interrupted))
	}
}

// streamRoom's own table, stated once so the exclusions are pinned as facts
// rather than as consequences of a frame.
func TestStreamRoomSplitsKeysAndNamesTheExclusions(t *testing.T) {
	for _, one := range []struct {
		key     string
		room    string
		suffix  string
		drawn   bool
		because string
	}{
		{"", "", "", true, "the empty key is this room's own, as it always was"},
		{"chat-1", "chat-1", "", true, "an unsuffixed key is the room's own turn"},
		{"chat-1#delivered", "chat-1", "#delivered", true, "the delivery answer becomes a row"},
		{"chat-1#receipt", "chat-1", "#receipt", true, "the receipt wake becomes a row"},
		{"chat-1#aside", "chat-1", "#aside", false, "an aside becomes a fold"},
		{"chat-1#later", "chat-1", "#later", false, "an unknown companion is refused, not guessed"},
	} {
		room, suffix, drawn := streamRoom(one.key)
		if room != one.room || suffix != one.suffix || drawn != one.drawn {
			t.Errorf("streamRoom(%q) = (%q, %q, %v), want (%q, %q, %v) — %s",
				one.key, room, suffix, drawn, one.room, one.suffix, one.drawn, one.because)
		}
	}
}
