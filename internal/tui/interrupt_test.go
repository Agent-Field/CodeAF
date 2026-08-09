package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// interruptCommander is the head as the surface sees it: something that can be
// asked to stop, and that says whether there was anything to stop.
type interruptCommander struct {
	*fakeCommander
	partials []string
	stops    bool
}

func (c *interruptCommander) Interrupt(partial string) bool {
	c.partials = append(c.partials, partial)
	return c.stops
}

// liveTurn is a window in the state the whole wave is about: a turn sent, a
// reply arriving, nothing of it settled yet.
func liveTurn(t *testing.T, partial string) (*Model, *interruptCommander) {
	t.Helper()
	commander := &interruptCommander{fakeCommander: newFakeCommander(), stops: true}
	model := NewWithCommander(&fakeBackend{}, "interrupt", commander)
	model.setSize(100, 30)
	model.noteAwaitingReply(store.Message{
		Seq: 40, SessionID: "interrupt", Role: store.RoleUser, Body: "how is the report coming along?",
	})
	if partial == "" {
		return model, commander
	}
	model.applyStreamEvent(StreamEvent{Kind: StreamStarted})
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"` + partial})
	for index := 0; index < 256 && model.streamAnimating(); index++ {
		model.advanceStream()
	}
	if !model.streamVisible() {
		t.Fatalf("the fixture never drew the partial: shown %q", model.streamShown)
	}
	return model, commander
}

// Escape used to quit the session while a reply was arriving. It stops the
// reply now, and it spends that press on nothing else: the draft is still
// there afterwards, and so is the window.
func TestEscapeStopsTheTurnBeforeItTouchesTheDraftOrQuits(t *testing.T) {
	model, commander := liveTurn(t, "the report is ")
	typeIntoModel(model, "no wait")

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command != nil {
		t.Fatal("escape quit the app with a turn in flight")
	}
	if len(commander.partials) != 1 || commander.partials[0] != "the report is" {
		t.Fatalf("the head was asked to stop with %q, want the words on screen", commander.partials)
	}
	if model.input.Value() != "no wait" {
		t.Fatalf("the same press wiped the draft: composer holds %q", model.input.Value())
	}
	if model.awaitingSeq != 0 || model.turnInFlight() {
		t.Fatalf("the window is still waiting on a turn it stopped: awaiting %d", model.awaitingSeq)
	}

	// Only now does the ladder move on: the draft is stashed by the second
	// press, and the third — with nothing in flight and nothing typed — quits.
	if _, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc}); command != nil || model.input.Value() != "" {
		t.Fatalf("the second escape did not clear the draft: %q", model.input.Value())
	}
	if _, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc}); command == nil {
		t.Fatal("escape never reaches quit once the turn is over and the draft is empty")
	}
}

// The words that arrived stay on screen, and the durable line the head posts —
// the same words plus the mark — lands on top of them rather than behind them.
// A stopped turn that rendered as nothing would be the cardinal sin twice over.
func TestTheStoppedReplyStaysOnScreenAndItsDurableLineLandsInPlace(t *testing.T) {
	model, _ := liveTurn(t, "the report is ")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if thread := ansi.Strip(model.renderMessages()); !strings.Contains(thread, "the report is") {
		t.Fatalf("the stopped reply vanished from the thread:\n%s", thread)
	}

	landed := store.Message{Seq: 41, SessionID: "interrupt", Role: store.RoleAgent,
		Body: "the report is\n\n— interrupted", Time: time.Now()}
	model.applyPoll(pollResultMsg{sessionID: "interrupt", messages: []store.Message{landed}})
	for index := 0; index < 512 && model.streamAnimating(); index++ {
		model.advanceStream()
	}
	if model.streamMode != streamNone || len(model.streamQueue) != 0 {
		t.Fatalf("the stopped stream still holds the lane: mode %d queued %d",
			model.streamMode, len(model.streamQueue))
	}
	thread := ansi.Strip(model.renderMessages())
	if !strings.Contains(thread, "interrupted") {
		t.Fatalf("the durable interrupted line never drew:\n%s", thread)
	}
	if strings.Count(thread, "the report is") != 1 {
		t.Fatalf("the stopped reply is on screen twice:\n%s", thread)
	}
}

// A turn stopped before a single word of the answer arrived is a turn nobody
// has been answered on. The words go back to the composer so they can be
// corrected — that is the whole reason people reach for the key.
func TestStoppingBeforeAnyReplyHandsTheTurnBack(t *testing.T) {
	commander := &interruptCommander{fakeCommander: newFakeCommander(), stops: true}
	model := NewWithCommander(&fakeBackend{}, "interrupt", commander)
	model.setSize(100, 30)
	typeIntoModel(model, "summarise the wrong file")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.noteAwaitingReply(store.Message{Seq: 12, SessionID: "interrupt", Role: store.RoleUser,
		Body: "summarise the wrong file"})

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(commander.partials) != 1 || commander.partials[0] != "" {
		t.Fatalf("a turn with nothing on screen carried a partial: %q", commander.partials)
	}
	if model.input.Value() != "summarise the wrong file" {
		t.Fatalf("the just-sent turn was not handed back: composer holds %q", model.input.Value())
	}

	// With words of the answer already on screen the exchange has happened, and
	// re-filling the composer would be undoing the wrong thing.
	answered, _ := liveTurn(t, "the file says ")
	_, _ = answered.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if answered.input.Value() != "" {
		t.Fatalf("a turn stopped mid-answer refilled the composer with %q", answered.input.Value())
	}
}

// Ctrl+C was an unconditional quit on the first line of the key handler. One
// mistimed press ended a session with a turn in it; now the first press stops
// the turn and says so, and the second inside the window means it.
func TestCtrlCStopsTheTurnAndOnlyThenQuits(t *testing.T) {
	model, commander := liveTurn(t, "the report is ")
	now := time.Now()
	model.standingNow = func() time.Time { return now }

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if command != nil {
		t.Fatal("the first ctrl+c quit with a turn in flight")
	}
	if len(commander.partials) != 1 {
		t.Fatalf("ctrl+c did not take the interrupt path: %v", commander.partials)
	}
	if !strings.Contains(model.status, "ctrl+c again") {
		t.Fatalf("nothing told the reader what the next press does: %q", model.status)
	}
	if _, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); command == nil {
		t.Fatal("the second ctrl+c inside the window did not quit")
	}

	// Outside the window it is a fresh intention, and an idle surface still
	// quits on the first press — nothing is lost there.
	now = now.Add(quitConfirmWindow + time.Second)
	idle := NewWithCommander(&fakeBackend{}, "idle", newFakeCommander())
	if _, command = idle.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); command == nil {
		t.Fatal("ctrl+c on an idle surface did not quit")
	}
}

// A window with no head behind it has nothing to stop. The key must still not
// leave the reader stuck: the state resets and the ladder carries on.
func TestStoppingAWindowWithNoHeadStillReleasesTheLadder(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "visitor", newFakeCommander())
	model.setSize(100, 30)
	model.noteAwaitingReply(store.Message{Seq: 3, SessionID: "visitor", Role: store.RoleUser, Body: "hello"})

	if _, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc}); command != nil {
		t.Fatal("escape quit while the window still believed a turn was live")
	}
	if model.turnInFlight() {
		t.Fatal("a window that cannot interrupt stayed convinced a turn was in flight")
	}
	if _, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc}); command == nil {
		t.Fatal("the next escape never reached quit")
	}
}

// ── input is never lost ─────────────────────────────────────────────────────

// Sent turns go into a ring the arrows walk. Up reaches back through them, down
// walks toward now, and the end of the walk is the empty line the person left.
func TestSentTurnsComeBackWithTheArrows(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "ring", newFakeCommander())
	model.setSize(100, 30)
	for _, turn := range []string{"first turn", "second turn"} {
		typeIntoModel(model, turn)
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if model.input.Value() != "" {
		t.Fatalf("sending left %q in the composer", model.input.Value())
	}

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.input.Value() != "second turn" {
		t.Fatalf("up recalled %q, want the newest turn", model.input.Value())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.input.Value() != "first turn" {
		t.Fatalf("up recalled %q, want the older turn", model.input.Value())
	}
	// The oldest line is the end of the walk; the arrow holds there rather than
	// scrolling the thread out from under a draft that is on screen.
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.input.Value() != "first turn" {
		t.Fatalf("past the oldest turn the composer holds %q", model.input.Value())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.input.Value() != "second turn" {
		t.Fatalf("down recalled %q, want the newer turn", model.input.Value())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.input.Value() != "" || model.recalling() {
		t.Fatalf("the walk did not end on the empty line: %q", model.input.Value())
	}

	// With nothing sent, the arrows keep their older job of reading the thread
	// backwards, and a visible option question still owns them first.
	fresh := NewWithCommander(&fakeBackend{}, "ring-empty", newFakeCommander())
	fresh.setSize(100, 30)
	fresh.chat.SetContent(strings.Repeat("line\n", 200))
	fresh.chat.SetYOffset(30)
	_, _ = fresh.Update(tea.KeyMsg{Type: tea.KeyUp})
	if fresh.chat.YOffset != 27 {
		t.Fatalf("up stopped scrolling the thread: offset %d, want 27", fresh.chat.YOffset)
	}
}

// Escape no longer destroys a draft: it stashes it, and the up arrow — ahead of
// everything already sent — is where it comes back from.
func TestEscapeStashesTheDraftAndTheUpArrowReturnsIt(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "stash", newFakeCommander())
	model.setSize(100, 30)
	typeIntoModel(model, "sent already")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	typeIntoModel(model, "half a thought")

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.input.Value() != "" {
		t.Fatalf("escape did not clear the draft: %q", model.input.Value())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.input.Value() != "half a thought" {
		t.Fatalf("the stashed draft did not come back: %q", model.input.Value())
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.input.Value() != "sent already" {
		t.Fatalf("older turns are unreachable behind the stash: %q", model.input.Value())
	}
}

// Three overlays borrowed the composer and reset it on the way open: settings,
// the model picker, and /history. Help and voice always gave the draft back;
// now they all do.
func TestOverlaysGiveTheBorrowedDraftBack(t *testing.T) {
	plain := func(t *testing.T) *Model {
		return NewWithCommander(&fakeBackend{}, "overlay", newFakeCommander())
	}
	for _, surface := range []struct {
		name  string
		build func(*testing.T) *Model
		open  func(*Model)
		close func(*Model)
	}{
		{
			name:  "settings",
			build: func(t *testing.T) *Model { model, _, _ := newSettingsModel(t); return model },
			open:  func(model *Model) { _ = model.openSettings() },
			close: func(model *Model) { model.closeSettings() },
		},
		{
			name:  "model picker",
			open:  func(model *Model) { _ = model.openModelPicker("talk") },
			close: func(model *Model) { model.closePalette() },
		},
		{
			name:  "models palette",
			open:  func(model *Model) { _ = model.openModelsPalette() },
			close: func(model *Model) { model.closePalette() },
		},
		{
			name: "history",
			open: func(model *Model) { _ = model.openRecallHistory("") },
			close: func(model *Model) {
				_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
			},
		},
	} {
		t.Run(surface.name, func(t *testing.T) {
			build := surface.build
			if build == nil {
				build = plain
			}
			model := build(t)
			model.setSize(100, 30)
			typeIntoModel(model, "the draft I was writing")
			surface.open(model)
			if strings.Contains(model.input.Value(), "the draft") {
				t.Fatalf("the overlay opened over the draft: %q", model.input.Value())
			}
			surface.close(model)
			if model.input.Value() != "the draft I was writing" {
				t.Fatalf("the draft was not returned: composer holds %q", model.input.Value())
			}
		})
	}
}

// A slash command is the door being opened, not a draft. It is dropped rather
// than stashed, so closing the overlay does not re-type the command.
func TestASlashCommandIsNotADraftAnOverlayOwes(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "slash", newFakeCommander())
	model.setSize(100, 30)
	typeIntoModel(model, "/model")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.closePalette()
	if model.input.Value() != "" {
		t.Fatalf("closing the picker re-typed the command: %q", model.input.Value())
	}
}
