package tui3

// THE SEND LEAVES THE EVENT LOOP, AND EVERYTHING BELOW IS WHAT THAT COSTS IF IT
// IS DONE CARELESSLY.
//
// A crossing that takes ten seconds must not take the keyboard with it; an
// answer that arrives after the person has walked into another task must not
// paint there; a correction queued behind another must cross to the engine it
// was typed at and not to whatever is answering when it is released; and a send
// nobody answered must stay a SEND — with the name it already has — because a
// sentence handed back to the box takes a new name on the next enter and can
// then be delivered twice.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// lostLink is a crossing NOBODY ANSWERED: the shape internal/remote's client
// gives a call whose link died with it outstanding.
type lostLink struct{}

func (lostLink) Error() string { return "the connection to devbox went away" }
func (lostLink) Unwrap() error { return session.ErrSendUnanswered }

// namedRoomFake is an engine that takes the name on a send. It is [roomFake]
// with the identity door on it, plus the two knobs these tests need: how many
// of the next crossings answer with a lost link, and a gate to hold one open.
type namedRoomFake struct {
	*roomFake
	mu sync.Mutex
	// repeat is what the engine says about recognising a send twice.
	repeat bool
	// lost is how many of the next crossings answer with a dead link.
	lost int
	// loseAck accepts the send, then loses this many receipts, including repeats.
	loseAck int
	// afterAckLoss refuses subsequent asks once the last receipt has been lost.
	afterAckLoss error
	// open is the conversation this engine currently has, when it is one that
	// checks. A send naming another one is refused and delivered nowhere.
	open string
	// crossings is every ask this engine saw, identity and all.
	crossings []session.SteerSource
	// took is what was actually DELIVERED — a repeat adds nothing to it.
	took []steerLine
	// seen is the receipt each named send was written down as.
	seen map[string]uint64
	next uint64
}

func namedEngine(t *testing.T, repeat bool) (*app, *namedRoomFake) {
	t.Helper()
	a, fake, _ := roomApp(t)
	named := &namedRoomFake{roomFake: fake, repeat: repeat, seen: map[string]uint64{}}
	a.agent = named
	return a, named
}

func (f *namedRoomFake) SteerRepeatKnown() bool { return f.repeat }

func (f *namedRoomFake) SteerTaskFrom(id uint64, text string, from session.SteerSource) (receipt session.SteerReceipt, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	defer func() {
		if err == nil && f.loseAck > 0 {
			f.loseAck--
			if f.loseAck == 0 && f.afterAckLoss != nil {
				f.roomFake.steerErr = f.afterAckLoss
			}
			receipt, err = session.SteerReceipt{}, lostLink{}
		}
	}()
	f.crossings = append(f.crossings, from)
	if f.lost > 0 {
		f.lost--
		return session.SteerReceipt{}, lostLink{}
	}
	if f.open != "" && from.Conversation != "" && from.Conversation != f.open {
		return session.SteerReceipt{}, session.ErrNotThatConversation
	}
	if f.roomFake.steerErr != nil {
		return session.SteerReceipt{}, f.roomFake.steerErr
	}
	key := fmt.Sprintf("%s/%d", from.Scope, from.Seq)
	if from.Scope != "" {
		if direction, ok := f.seen[key]; ok {
			return session.SteerReceipt{Again: true, Direction: direction,
				Landing: "already on the task's record from the same message — nothing was sent a second time"}, nil
		}
	}
	f.next++
	f.seen[key] = f.next
	f.took = append(f.took, steerLine{id: id, text: text})
	return session.SteerReceipt{Direction: f.next, Landing: session.SteerDelivered(false)}, nil
}

// delivered is what this engine actually handed a worker.
func (f *namedRoomFake) delivered() []steerLine {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]steerLine(nil), f.took...)
}

// asked is every crossing, repeats included.
func (f *namedRoomFake) asked() []session.SteerSource {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]session.SteerSource(nil), f.crossings...)
}

// typeSteer puts a sentence in the box and presses enter, WITHOUT running the
// command the keypress hands back. That command is the crossing, so holding it
// is a send that is still on its way — which is the state every test in this
// file is about, and it is a state and not a sleep. The record write which
// releases that crossing is run here too, so a harness that loses its answer is
// reported at that boundary rather than as a later claim that no send crossed.
func typeSteer(t *testing.T, a *app, line string) tea.Cmd {
	t.Helper()
	a.input.setText(line)
	done := make(chan tea.Cmd, 1)
	go func() { done <- a.steer() }()
	select {
	case cmd := <-done:
		// THE RECORD IS WRITTEN BEFORE THE WIRE, so what the keypress hands back is
		// the write and the crossing is what its answer starts (steersend.go's
		// [app.keepSendFirst]). This runs that write and holds the crossing, which
		// is the state every test below is about.
		return steerAfterKeeping(t, a, cmd)
	case <-time.After(5 * time.Second):
		t.Fatal("enter in a task's page waited for the engine — the keypress is calling the far machine")
		return nil
	}
}

// steerAfterKeeping lets the send's own record write finish and hands back the
// crossing it releases. Nothing else in the keypress's batch is a message the
// surface has to see — the rest is a repaint and the ordinary debounce. A send
// still waiting when no write answer came back is the harness's failure and is
// named here; a write which releases no crossing remains legitimate when the
// send is queued behind another one.
func steerAfterKeeping(t *testing.T, a *app, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	var out []tea.Cmd
	kept := 0
	for _, msg := range runCmd(cmd) {
		written, ok := msg.(draftKeptMsg)
		if !ok {
			continue
		}
		kept++
		if written.err != nil {
			t.Fatalf("the send's own record could not be written: %v", written.err)
		}
		_, crossing := a.Update(written)
		out = append(out, crossing)
	}
	if kept == 0 && len(a.outbox.waiting) > 0 {
		t.Fatal("the send's own record write never answered the harness; the send is still waiting on that write, so this is the harness's news and not the surface refusing to cross it")
	}
	if len(out) == 0 {
		return nil
	}
	return tea.Batch(out...)
}

// deliver runs a held crossing and lets the surface take its answer.
func deliver(t *testing.T, a *app, cmd tea.Cmd) {
	t.Helper()
	drive(t, a, runCmd(cmd)...)
}

// steerRows is every correction drawn on the page on screen.
func steerRows(a *app) []steerElbow {
	var out []steerElbow
	if a.room == nil {
		return out
	}
	for _, e := range a.room.entries {
		if e.kind == entrySteer && e.steer != nil {
			out = append(out, *e.steer)
		}
	}
	return out
}

// A SLOW CROSSING KEEPS THE KEYBOARD. The words are on the page saying they are
// on their way, the box is empty, and esc leaves the room while the engine has
// still said nothing.
func TestASendInFlightKeepsTheKeyboardAndTheRoom(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	cmd := typeSteer(t, a, "the config lives under etc/")

	if rows := steerRows(a); len(rows) != 1 || rows[0].consumed {
		t.Fatalf("the page draws %+v, want one correction that has not been answered", rows)
	}
	if !strings.Contains(roomText(a), steerSendingWord) {
		t.Fatalf("a crossing in flight says nothing about itself:\n%s", roomText(a))
	}
	if a.input.String() != "" {
		t.Fatalf("the box still holds %q while the send crosses", a.input.String())
	}
	if len(engine.delivered()) != 0 {
		t.Fatal("the keypress delivered the words itself, so the crossing was inside the event loop")
	}
	// THE WHOLE POINT: the surface still answers while the engine has not.
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did nothing while a send was crossing — the keyboard was waiting on the engine")
	}

	deliver(t, a, cmd)
	if got := engine.delivered(); len(got) != 1 || got[0].text != "the config lives under etc/" || got[0].id != 7 {
		t.Fatalf("the task received %v, want the one correction exactly once", got)
	}
	// AND NOTHING WAS PAINTED WHERE THE PERSON NOW IS. The conversation never
	// carried this sentence and must not gain it from an answer.
	for _, e := range a.entries {
		if e.kind == entrySteer && e.steer != nil {
			t.Fatalf("a task's answer drew a correction into the conversation: %q", e.steer.words)
		}
	}
}

// AND AN ANSWER SETTLES THE PAGE IT WAS TYPED INTO AND NO OTHER. Walking into
// another task while a send crosses must not put its receipt on that task's
// page.
func TestAnAnswerSettlesOnlyThePageItWasTypedInto(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	cmd := typeSteer(t, a, "the config lives under etc/")

	// A second node, and the person walks into its page while the first send is
	// still crossing.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Port the parser",
		session.TaskRunning, session.TaskNotice{})})
	a.openRoom(8, "Port the parser")
	if a.room == nil || a.room.id != 8 {
		t.Fatal("the second task's page did not open")
	}

	deliver(t, a, cmd)

	if rows := steerRows(a); len(rows) != 0 {
		t.Fatalf("task 8's page drew %+v — an answer painted into the wrong room", rows)
	}
	if got := engine.delivered(); len(got) != 1 || got[0].id != 7 {
		t.Fatalf("the words went to %v, want task 7 alone", got)
	}
}

// A QUEUED SEND CROSSES TO THE ENGINE IT WAS TYPED AT. The window moved to
// another conversation while the first was still crossing; the second must not
// be handed to whatever answers to that number over there.
func TestAQueuedSendCrossesToTheEngineItWasTypedAt(t *testing.T) {
	a, engine := namedEngine(t, true)
	a.file = "/srv/one.jsonl"
	clickRail(t, a, 0)
	first := typeSteer(t, a, "the config lives under etc/")
	second := typeSteer(t, a, "and sort it by date")
	if second != nil {
		t.Fatal("the second send crossed before the first was answered")
	}
	if len(steerRows(a)) != 2 || len(a.steerSnapshots(a.file, 7)) != 2 {
		t.Fatal("the queued correction was not retained on its page and in its record")
	}
	if len(engine.asked()) != 0 {
		t.Fatal("a send crossed before the one in front of it was answered")
	}

	// The window is now sitting in another conversation, on another engine, whose
	// task 7 is somebody else's work entirely.
	elsewhere := &namedRoomFake{roomFake: engine.roomFake, repeat: true, seen: map[string]uint64{}}
	a.agent = elsewhere
	a.file = "/srv/two.jsonl"

	deliver(t, a, first)

	if got := elsewhere.asked(); len(got) != 0 {
		t.Fatalf("the queued correction crossed to the conversation the window had moved to: %+v", got)
	}
	got := engine.delivered()
	if len(got) != 2 || got[0].text != "the config lives under etc/" || got[1].text != "and sort it by date" {
		t.Fatalf("the engine it was typed at received %v, want both corrections in the order they were typed", got)
	}
}

// A SEND NOBODY ANSWERED STAYS A SEND. Its words stay on the page, its name
// stays with it, and asking again is a retry OF THAT SEND — so the correction
// lands exactly once however many times it is asked about.
func TestAnUnansweredSendIsRetriedUnderItsOwnName(t *testing.T) {
	a, engine := namedEngine(t, true)
	// Both the first crossing and the automatic second one find nobody there.
	engine.lost = 2
	clickRail(t, a, 0)
	deliver(t, a, typeSteer(t, a, "make it CSV"))

	if asked := engine.asked(); len(asked) != 2 {
		t.Fatalf("the engine was asked %d times, want the crossing and its one automatic repeat", len(asked))
	}
	rows := steerRows(a)
	if len(rows) != 1 || rows[0].consumed || !rows[0].stalled {
		t.Fatalf("the page draws %+v, want the words kept with an unresolved clause", rows)
	}
	if rows[0].words != "make it CSV" {
		t.Fatalf("the page lost the person's words: %+v", rows[0])
	}
	if !strings.Contains(roomText(a), steerLostWord) {
		t.Fatalf("the page does not say the outcome is unknown:\n%s", roomText(a))
	}
	// THE WORDS ARE NOT HANDED BACK TO THE BOX. A sentence in the box takes a NEW
	// name on the next enter, and this one may already be on the task's record.
	if a.input.String() != "" {
		t.Fatalf("an unanswered send was demoted to text in the box (%q), which is how one correction becomes two", a.input.String())
	}
	if !a.guarding() {
		t.Fatal("nothing offered a way to ask again")
	}
	if !strings.Contains(plain(strings.Join(a.guardRows(a.bodyWidth()), "\n")), "[r]") {
		t.Fatalf("the question offers no action:\n%s", strings.Join(a.guardRows(a.bodyWidth()), "\n"))
	}

	// The link came back. `r` asks again about the same send.
	model, cmd := a.Update(key("r"))
	a = model.(*app)
	deliver(t, a, cmd)

	asked := engine.asked()
	if len(asked) != 3 {
		t.Fatalf("the engine saw %d crossings, want the two lost ones and the retry", len(asked))
	}
	for i, from := range asked {
		if from.Scope == "" || from.Seq == 0 {
			t.Fatalf("crossing %d carried no name (%+v), so no engine could recognise it twice", i, from)
		}
		if from != asked[0] {
			t.Fatalf("the retry crossed under %+v, want the name the send already had (%+v)", from, asked[0])
		}
	}
	if got := engine.delivered(); len(got) != 1 || got[0].text != "make it CSV" {
		t.Fatalf("the task received %v, want the correction exactly once", got)
	}
	rows = steerRows(a)
	if len(rows) != 1 || !rows[0].consumed || rows[0].stalled {
		t.Fatalf("after the retry the page draws %+v, want one settled correction", rows)
	}
	if !strings.Contains(roomText(a), session.SteerDelivered(false)) {
		t.Fatalf("the settled row does not carry the engine's own receipt:\n%s", roomText(a))
	}
	if a.unsureSteer() != nil {
		t.Fatal("a send that was answered is still being held as unresolved")
	}
}

// AND AN UNRESOLVED SEND CAN BE WRITTEN DOWN AND TAKEN BACK WITH ITS NAME. This
// is the seam durable recipient state keeps it through: a restored send that
// took a fresh name would be a new correction to an engine that may already
// hold the old one.
func TestAnUnresolvedSendRoundTripsThroughItsKeptValue(t *testing.T) {
	a, engine := namedEngine(t, true)
	a.file = "/srv/one.jsonl"
	engine.lost = 2
	clickRail(t, a, 0)
	deliver(t, a, typeSteer(t, a, "make it CSV"))

	kept := a.steerSnapshots("/srv/one.jsonl", 7)
	if len(kept) != 1 {
		t.Fatalf("the page is holding %d unsettled sends, want the one nobody answered", len(kept))
	}
	if kept[0].scope == "" || kept[0].seq == 0 || kept[0].words != "make it CSV" {
		t.Fatalf("the kept send lost its name or its words: %+v", kept[0])
	}
	if kept[0].state != draftSendUnanswered {
		t.Fatalf("the kept send was written down as %q, want the state it is actually in", kept[0].state)
	}
	// A PAGE THAT HAS NOTHING UNSETTLED WRITES NOTHING DOWN, so a record is not
	// grown by every page somebody visited.
	if held := a.steerSnapshots("/srv/one.jsonl", 8); len(held) != 0 {
		t.Fatalf("another page reported %+v", held)
	}

	// The window closed and came back. Nothing else survived; this did.
	next, _ := namedEngine(t, true)
	next.file = "/srv/one.jsonl"
	next.restoreSteerSnapshots("/srv/one.jsonl", 7, kept)
	back := next.steerSnapshots("/srv/one.jsonl", 7)
	if len(back) != 1 || back[0].scope != kept[0].scope || back[0].seq != kept[0].seq {
		t.Fatalf("the restored send is %+v, want the one that was written down", back)
	}
	if !back[0].at.Equal(kept[0].at) || back[0].line != kept[0].line {
		t.Fatalf("the restored send lost the instant or the sentence: %+v", back[0])
	}
	// AND IT KNOWS WHICH CONVERSATION IT WAS WRITTEN FOR, so the engine can refuse
	// it if that conversation is no longer the one open.
	held := next.outbox.unsure[steerAddress{owner: next.steerOwner(), task: 7}]
	if len(held) != 1 || held[0].from.Conversation != "/srv/one.jsonl" {
		t.Fatalf("the restored send names conversation %q", held[0].from.Conversation)
	}

	// AND ANOTHER CONVERSATION'S RECORD IS NOT LAID OUT HERE. Its sends belong to
	// a window that is not this one.
	next.restoreSteerSnapshots("/srv/two.jsonl", 7, kept)
	if got := len(next.outbox.unsure[steerAddress{owner: next.steerOwner(), task: 7}]); got != 1 {
		t.Fatalf("another conversation's record was restored onto this page: %d sends held", got)
	}

	// Closing is not a receipt. The original name remains available if this
	// conversation is resumed, and nothing is sent merely because it closed.
	next.forgetSteerOwner(next.steerOwner())
	if left := next.steerSnapshots("/srv/one.jsonl", 7); len(left) != 1 || left[0].scope != kept[0].scope || left[0].seq != kept[0].seq {
		t.Fatalf("closing discarded or renamed the unanswered correction: %+v", left)
	}
}

// AND A NEW SENTENCE IS A NEW SEND, whatever is unresolved beside it. Two
// intentional corrections are two corrections even when they read the same.
func TestASecondIntentionalSendTakesItsOwnName(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	deliver(t, a, typeSteer(t, a, "try it again"))
	deliver(t, a, typeSteer(t, a, "try it again"))

	asked := engine.asked()
	if len(asked) != 2 {
		t.Fatalf("the engine saw %d crossings, want one per enter", len(asked))
	}
	if asked[0].Seq == asked[1].Seq {
		t.Fatalf("two deliberate sends shared one name (%+v), so the second would be dropped as a repeat", asked[0])
	}
	if got := engine.delivered(); len(got) != 2 {
		t.Fatalf("the task received %v, want both corrections", got)
	}
}

// A REFUSED SEND GIVES THE WHOLE COMPOSER BACK — the words as they were
// spelled, the caret where it was left, and the compact paste chips the text
// stands on — and takes its row off the page.
func TestARefusedSendGivesTheWholeComposerBack(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.steerErr = errors.New("task 7 is done, not running")
	clickRail(t, a, 0)
	a.input.setText("make it CSV")
	a.input.cursor = 4
	a.pastes = []pasteChip{{n: 1, text: "a document the sentence stands on"}}

	deliver(t, a, mustSteer(t, a))

	if a.input.String() != "make it CSV" {
		t.Fatalf("the box holds %q, want the sentence the engine refused", a.input.String())
	}
	if a.input.cursor != 4 {
		t.Fatalf("the caret came back at %d, want where the person left it (4)", a.input.cursor)
	}
	if len(a.pastes) != 1 || a.pastes[0].text != "a document the sentence stands on" {
		t.Fatalf("the compact paste was lost: %+v", a.pastes)
	}
	if rows := steerRows(a); len(rows) != 0 {
		t.Fatalf("the refused correction is still drawn on the page: %+v", rows)
	}
	if !a.guarding() || a.guard.text != "make it CSV" {
		t.Fatalf("guard = %+v, want the engine's refusal over the person's own words", a.guard)
	}
}

// AND IT NEVER OVERWRITES A NEWER DRAFT. A sentence started while the send was
// crossing is the person's too; the refused one is kept for that page instead.
func TestARefusedSendIsKeptWhenTheBoxHasMovedOn(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.steerErr = errors.New("task 7 is done, not running")
	clickRail(t, a, 0)
	cmd := typeSteer(t, a, "make it CSV")
	a.input.setText("something else entirely")

	deliver(t, a, cmd)

	if a.input.String() != "something else entirely" {
		t.Fatalf("the box holds %q — a refused send overwrote a newer draft", a.input.String())
	}
	// IT IS KEPT ON THAT RECIPIENT'S OWN COMPOSER, as words the person can have
	// back and not as a send anybody could deliver again (recipient.go).
	kept, ok := a.takeRecovered(taskRecipient(7))
	if !ok || kept.line != "make it CSV" {
		t.Fatalf("the refused sentence was not kept for its page: %+v %v", kept, ok)
	}
	if kept.state != draftSendRefused {
		t.Fatalf("the refused sentence is held as %q, want it to be the person's again", kept.state)
	}
	if _, twice := a.takeRecovered(taskRecipient(7)); twice {
		t.Fatal("the kept sentence was handed out twice")
	}
}

// A STALE OR DUPLICATE ANSWER RELEASES NOTHING. One that matched no send would
// otherwise send the next correction early and leave the one in flight with
// nothing to settle it.
func TestAStaleAnswerReleasesNothing(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	first := typeSteer(t, a, "the config lives under etc/")
	typeSteer(t, a, "and sort it by date")

	at := steerAddress{owner: a.steerOwner(), task: 7}
	flying := a.outbox.flight[at]
	if flying == 0 {
		t.Fatal("nothing is recorded as crossing")
	}

	// An answer for a send this surface is not holding, and one for the right
	// send with the wrong address.
	drive(t, a, steerSentMsg{key: flying + 99, at: at, asked: 1})
	drive(t, a, steerSentMsg{key: flying, at: steerAddress{owner: at.owner, task: 8}, asked: 1})
	if len(engine.asked()) != 0 {
		t.Fatalf("a stale answer released the queue: %+v", engine.asked())
	}
	if a.outbox.flight[at] != flying {
		t.Fatal("a stale answer took the crossing off the outbox")
	}

	deliver(t, a, first)
	if len(engine.delivered()) != 2 {
		t.Fatalf("after the real answer the engine has %v, want both corrections", engine.delivered())
	}
	// AND THE REAL ANSWER ARRIVING TWICE CHANGES NOTHING. A second copy of an
	// answer already spent must not cross anything again.
	drive(t, a, steerSentMsg{key: flying, at: at, asked: 1})
	if len(engine.delivered()) != 2 {
		t.Fatalf("a duplicate answer moved the outbox: the engine now has %v", engine.delivered())
	}
	if len(engine.asked()) != 2 {
		t.Fatalf("a duplicate answer made another crossing: %d in all", len(engine.asked()))
	}
}

// AND A SEND WHOSE OUTCOME WAS NEVER LEARNED COMES BACK ONTO ITS PAGE, with the
// name it was made under — the process that was holding it is gone, and the
// correction is not.
func TestARestoredUnresolvedSendKeepsItsNameAndItsPage(t *testing.T) {
	a, engine := namedEngine(t, true)
	a.file = "/srv/one.jsonl"
	said := time.Now().Add(-time.Hour)
	a.restoreSteerSnapshots("/srv/one.jsonl", 7, []outboxSnapshot{{
		scope: "yesterday", seq: 4, at: said, state: draftSendUnanswered,
		line: "make it CSV", words: "make it CSV",
	}})

	clickRail(t, a, 0)
	rows := steerRows(a)
	if len(rows) != 1 || !rows[0].stalled || rows[0].words != "make it CSV" {
		t.Fatalf("the page draws %+v, want the unresolved correction back on it", rows)
	}
	// AND THE OFFER COMES WITH IT. A row saying nobody answered and offering
	// nothing would be a dead end.
	if a.unsureSteer() == nil {
		t.Fatal("the restored send is not being held")
	}
	if !a.guarding() {
		t.Fatal("walking into the page offered no way to resolve the correction on it")
	}

	model, cmd := a.Update(key("r"))
	a = model.(*app)
	deliver(t, a, cmd)

	asked := engine.asked()
	if len(asked) != 1 {
		t.Fatalf("the retry made %d crossings, want one", len(asked))
	}
	if asked[0].Scope != "yesterday" || asked[0].Seq != 4 || !asked[0].At.Equal(said) {
		t.Fatalf("the retry crossed under %+v, want the name the send was made with", asked[0])
	}
	// AND IT NAMES THE CONVERSATION IT WAS WRITTEN FOR. A restored retry with no
	// claim on it would be checked against nothing, which is the one thing a send
	// carried across a restart cannot afford.
	if asked[0].Conversation != "/srv/one.jsonl" {
		t.Fatalf("the restored retry named conversation %q", asked[0].Conversation)
	}
	if got := engine.delivered(); len(got) != 1 || got[0].text != "make it CSV" {
		t.Fatalf("the task received %v, want the restored correction once", got)
	}

	// AND A NEW SEND AFTER IT TAKES THIS LIFE'S OWN NAME, never the restored one.
	deliver(t, a, mustSteerText(t, a, "and sort it by date"))
	asked = engine.asked()
	if len(asked) != 2 {
		t.Fatalf("the engine saw %d crossings, want the retry and the new send", len(asked))
	}
	if asked[1].Scope == "yesterday" {
		t.Fatalf("a new send reused a restored name (%+v), which an engine would answer as a repeat and never deliver", asked[1])
	}
}

// A SEND THE ENGINE REFUSES AS ANOTHER CONVERSATION'S IS KEPT, NOT RE-AIMED.
// The conversation moved under the surface while the crossing was in hand; the
// correction is not delivered to the task with that number over there, it is not
// tipped into the composer of a conversation it was not written for, and it
// stays exactly where it was typed with the name it already has.
func TestASendRefusedAsAnotherConversationsIsKeptAndNeverReAimed(t *testing.T) {
	a, engine := namedEngine(t, true)
	a.file = "/srv/one.jsonl"
	engine.open = "/srv/one.jsonl"
	clickRail(t, a, 0)
	// The address the send is made at, taken while that conversation is the one
	// open — which is the only moment this surface can name it.
	one := steerAddress{owner: a.steerOwner(), task: 7}
	cmd := typeSteer(t, a, "make it CSV")

	// The person opened another conversation while the crossing was in hand, and
	// the engine behind the same handle went with them.
	a.file = "/srv/two.jsonl"
	engine.open = "/srv/two.jsonl"
	deliver(t, a, cmd)

	if got := engine.delivered(); len(got) != 0 {
		t.Fatalf("the conversation that REPLACED it was corrected: %v", got)
	}
	if a.input.String() != "" {
		t.Fatalf("the words were tipped into another conversation's box: %q", a.input.String())
	}
	held := a.outbox.unsure[one]
	if len(held) != 1 || held[0].words != "make it CSV" {
		t.Fatalf("the refused send is being held as %+v, want it kept where it was typed", held)
	}
	if held[0].from.Conversation != "/srv/one.jsonl" {
		t.Fatalf("the kept send now names %q", held[0].from.Conversation)
	}
	if rows := steerRows(a); len(rows) != 1 || rows[0].consumed || rows[0].landing != steerElsewhereWord {
		t.Fatalf("the page draws %+v, want the correction still on it saying it was not sent", rows)
	}

	// AND GOING BACK IS WHAT SENDS IT. The retry crosses under the same name, to
	// the conversation it was written for, and lands once.
	a.file = "/srv/one.jsonl"
	engine.open = "/srv/one.jsonl"
	deliver(t, a, a.retrySteer(one))
	if got := engine.delivered(); len(got) != 1 || got[0].text != "make it CSV" {
		t.Fatalf("the task received %v, want the correction exactly once", got)
	}
	asked := engine.asked()
	if len(asked) != 2 || asked[0].Seq != asked[1].Seq {
		t.Fatalf("the retry crossed under %+v, want the name the send already had", asked)
	}
	if a.unsureSteer() != nil {
		t.Fatal("a send that landed is still being held as unresolved")
	}
}

// A REFUSAL DOES NOT SETTLE A SEND NOBODY EVER ANSWERED FOR. The words may
// already be on the task's record from the crossing that was lost, so they are
// not handed back to the box — the next enter would name them afresh and the
// task could hear them twice.
func TestARefusalAfterALostCrossingKeepsTheSend(t *testing.T) {
	a, engine := namedEngine(t, true)
	engine.lost = 2
	clickRail(t, a, 0)
	deliver(t, a, typeSteer(t, a, "make it CSV"))
	if a.unsureSteer() == nil {
		t.Fatal("a crossing nobody answered is not being held")
	}

	// The link came back and the task had finished in the meantime.
	engine.mu.Lock()
	engine.roomFake.steerErr = errors.New("task 7 is done, not running")
	engine.mu.Unlock()
	deliver(t, a, a.retrySteer(steerAddress{owner: a.steerOwner(), task: 7}))

	if a.input.String() != "" {
		t.Fatalf("an uncertain send's words went back to the box as %q, where the next enter would rename them", a.input.String())
	}
	if a.unsureSteer() == nil {
		t.Fatal("a refusal settled a send whose earlier crossing nobody answered")
	}
	if rows := steerRows(a); len(rows) != 1 || rows[0].consumed || rows[0].landing != steerLostWord {
		t.Fatalf("the page draws %+v, want the correction still unresolved on it", rows)
	}
}

// A CONVERSATION WITH NO TRANSCRIPT HAS NO SENDER AT ALL. Its identity lives in
// this window only, so a correction sent under it could never be written down,
// asked about again, or recognised after a restart — and an unsafe repeat is
// worse than a refusal a person can see.
func TestAConversationWithNoTranscriptCannotSteer(t *testing.T) {
	a, _ := namedEngine(t, true)
	a.file = ""
	if a.canSteerTask() {
		t.Fatal("a conversation this build cannot name still had an ear to send into")
	}

	// AND A SAVED ONE CARRIES THE MACHINE IT IS ON. The same transcript path
	// names different work on two machines.
	a.file = "/srv/app/.aforge/one.jsonl"
	here := a.steerOwner()
	if !a.canSteerTask() {
		t.Fatal("a named conversation could not steer")
	}
	a.host = "devbox"
	if there := a.steerOwner(); there == here {
		t.Fatalf("the same path on another machine took the same name (%q)", there)
	}
}

// AND THE ENGINE'S OWN REFUSAL AFTER THE SAME CLOSE ENDS THE SAME WAY. The
// `not that conversation` ending keeps a send for the walk back to its page;
// after a real close there is no walk back, and `your words are kept on its
// page` is the same false sentence one error over.
func TestARefusedSendWhoseConversationWasClosedKeepsTheWordsOutLoud(t *testing.T) {
	a, engine := namedEngine(t, true)
	a.file = "/srv/one.jsonl"
	engine.open = "/srv/one.jsonl"
	clickRail(t, a, 0)
	one := steerAddress{owner: a.steerOwner(), task: 7}
	cmd := typeSteer(t, a, "make it CSV")

	// Closed for real, and the engine behind the same handle has moved on too,
	// so the crossing comes back [session.ErrNotThatConversation].
	a.closeRoom()
	a.forgetSteerOwner(one.owner)
	a.file = "/srv/two.jsonl"
	engine.open = "/srv/two.jsonl"

	deliver(t, a, cmd)

	if got := engine.delivered(); len(got) != 0 {
		t.Fatalf("a closed conversation's correction was delivered somewhere: %v", got)
	}
	if noted(a, steerElsewhereAway) {
		t.Fatal("the conversation claims the words are kept on a page that was closed")
	}
	if !noted(a, steerKeptNowhere) || !noted(a, "make it CSV") {
		t.Fatal("the words of a closed conversation's correction were dropped rather than said")
	}
	if len(a.outbox.unsure) != 0 {
		t.Fatalf("a closed conversation's send is still held for a retry: %+v", a.outbox.unsure)
	}
	if a.input.String() != "" {
		t.Fatalf("the words were tipped into another conversation's box: %q", a.input.String())
	}
}

// mustSteer presses enter on whatever is in the box and insists something was
// sent.
func mustSteer(t *testing.T, a *app) tea.Cmd {
	t.Helper()
	cmd := a.steer()
	if cmd == nil {
		t.Fatal("enter in a task's page sent nothing at all")
	}
	return cmd
}

func mustSteerText(t *testing.T, a *app, line string) tea.Cmd {
	t.Helper()
	a.input.setText(line)
	return mustSteer(t, a)
}
