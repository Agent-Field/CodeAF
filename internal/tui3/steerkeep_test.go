package tui3

// A CORRECTION IS ON DISK BEFORE IT IS ON THE WIRE, AND IT BELONGS TO THE PAGE
// IT WAS TYPED INTO (steersend.go, recipient.go, draftkeep.go).
//
// steersend_test.go pins what the outbox does while a window is open. These pin
// the half that outlives it: that nothing crosses before its own record has
// landed, that a send which could not be written down is not sent at all, that
// what comes back after a restart comes back UNDER THE NAME IT WAS SENT WITH,
// and that an answer arriving after the person has moved the whole window
// settles the conversation it was typed in rather than the one in front of them.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// draftKeptFrom is the record write out of a keypress's batch. Everything else
// the keypress hands back is a repaint or the ordinary debounce.
func draftKeptFrom(t *testing.T, msgs []tea.Msg) draftKeptMsg {
	t.Helper()
	for _, msg := range msgs {
		if kept, ok := msg.(draftKeptMsg); ok {
			return kept
		}
	}
	t.Fatal("the keypress wrote no record at all, so nothing could have been kept before the crossing")
	return draftKeptMsg{}
}

// keptSendsFor is what one page's slot on disk is holding.
func keptSendsFor(t *testing.T, a *app, who recipient) []draftKeepSend {
	t.Helper()
	slot, held := keptSlotFor(t, draftKeepPath(a.draftFile), who)
	if !held {
		return nil
	}
	return slot.Sends
}

// waitingWrite is the record write one send is waiting on, taken straight off
// the outbox so a test can run it by hand — held open, or out of order. It is
// the same value the keypress handed back, and only one of the two is ever run.
func waitingWrite(t *testing.T, a *app, key uint64) tea.Cmd {
	t.Helper()
	send := a.outbox.waiting[key]
	if send == nil {
		t.Fatalf("no correction is waiting to be written down under key %d", key)
		return nil
	}
	return send.kept.command()
}

// drainSave runs a keypress's batch WITH NO BUDGET, skipping its clocks. It is
// for the tests below that hold a record's writer open on purpose: [runCmd]'s
// budget exists so the suite never waits on a command nothing will answer, and
// here the test itself is what answers — so waiting is the point, and a budget
// would turn the write it is holding into a dropped message.
func drainSave(cmd tea.Cmd) []tea.Msg {
	if cmd == nil || cmdSymbol(cmd) == teaTickSymbol {
		return nil
	}
	switch produced := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, one := range produced {
			out = append(out, drainSave(one)...)
		}
		return out
	default:
		return []tea.Msg{produced}
	}
}

// NOTHING CROSSES BEFORE IT IS ON DISK. The keypress hands back the record's own
// write, and the crossing is what that write's answer starts — so a window that
// dies between the two leaves a correction that was never sent and is written
// down, rather than one that was sent and is not.
func TestASendIsOnDiskBeforeItIsOnTheWire(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	a.input.setText("make it CSV")
	a.input.cursor = 4

	cmd := a.steer()
	if cmd == nil {
		t.Fatal("enter in a task's page sent nothing at all")
	}
	if len(engine.asked()) != 0 {
		t.Fatal("the keypress crossed to the engine before anything was written down")
	}

	kept := draftKeptFrom(t, runCmd(cmd))
	if kept.err != nil {
		t.Fatalf("the send's own record could not be written: %v", kept.err)
	}
	if len(engine.asked()) != 0 {
		t.Fatal("the send crossed before its own write had been answered")
	}
	sends := keptSendsFor(t, a, taskRecipient(7))
	if len(sends) != 1 {
		t.Fatalf("the record holds %+v, want the crossing that has not been answered", sends)
	}
	// THE WHOLE NAME AND THE WHOLE COMPOSER, because both are what makes asking
	// again a repeat of this send rather than a second correction.
	one := sends[0]
	if one.Line != "make it CSV" || one.Words != "make it CSV" || one.Caret != 4 {
		t.Fatalf("the record holds %+v, want the sentence as it was typed", one)
	}
	if one.State != draftSendCrossing || one.Scope == "" || one.Seq == 0 || one.At == "" {
		t.Fatalf("the record holds %+v, want a send with its own durable name", one)
	}

	drive(t, a, kept)
	if got := engine.delivered(); len(got) != 1 || got[0].text != "make it CSV" {
		t.Fatalf("the task received %v, want the correction once the record had landed", got)
	}
}

// AND A SEND THAT COULD NOT BE WRITTEN DOWN IS NOT SENT. The words come back to
// the page they were typed on and the surface says why, because a correction the
// person believes was delivered is the one outcome this cannot afford.
func TestASendThatCannotBeWrittenDownNeverCrosses(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	// A directory at the temporary path fails the write for everybody, root
	// included.
	blocked := draftKeepPath(a.draftFile) + ".writing-" + strconv.Itoa(draftOwner)
	if err := os.MkdirAll(filepath.Dir(blocked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}

	a.input.setText("make it CSV")
	cmd := a.steer()
	if cmd == nil {
		t.Fatal("enter in a task's page sent nothing at all")
	}
	kept := draftKeptFrom(t, runCmd(cmd))
	if kept.err == nil {
		t.Fatal("the fixture's write was expected to fail")
	}
	// The write tidies its own temporary path on the way out, so this may already
	// be gone.
	_ = os.Remove(blocked)

	drive(t, a, kept)

	if len(engine.asked()) != 0 {
		t.Fatalf("a correction crossed although it could not be written down first: %+v", engine.asked())
	}
	if got := a.input.String(); got != "make it CSV" {
		t.Fatalf("the box holds %q, want the words back on the page they were typed on", got)
	}
	if !noted(a, steerUnwrittenWord) {
		t.Fatal("nothing was said about the correction that was not sent")
	}
	if rows := steerRows(a); len(rows) != 0 {
		t.Fatalf("the page still draws a correction that never left: %+v", rows)
	}
}

// TWO CORRECTIONS TYPED IN A HURRY CROSS IN THE ORDER THEY WERE TYPED, and the
// sentence started while they were both still being written down is untouched.
// One record carries both, so the newer write answers for the older send too.
func TestQueuedSendsCrossInOrderAndKeepANewerDraft(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)

	a.input.setText("the config lives under etc/")
	first := a.steer()
	a.input.setText("and sort it by date")
	second := a.steer()
	if first == nil || second == nil {
		t.Fatal("one of the two enters sent nothing at all")
	}
	// And they keep typing while neither write has been answered.
	a.input.setText("a third sentence, still being written")

	// The second write lands first. It is the whole composer, so it carries both
	// sends and releases both.
	drive(t, a, draftKeptFrom(t, runCmd(second)))
	got := engine.delivered()
	if len(got) != 2 || got[0].text != "the config lives under etc/" || got[1].text != "and sort it by date" {
		t.Fatalf("the task received %v, want both corrections in the order they were typed", got)
	}
	// The overtaken write arrives afterwards and releases nothing a second time.
	drive(t, a, draftKeptFrom(t, runCmd(first)))
	if again := engine.delivered(); len(again) != 2 {
		t.Fatalf("a superseded write sent something again: %v", again)
	}
	if box := a.input.String(); box != "a third sentence, still being written" {
		t.Fatalf("the box holds %q — the sends took the sentence being typed beside them", box)
	}
}

// A SEND NOBODY ANSWERED COMES BACK FROM DISK WITH THE NAME IT WAS SENT UNDER,
// and comes back as a SEND. Handed to the box instead, the next enter would mint
// a fresh name for it and the worker could read one correction twice.
func TestAnUnansweredSendComesBackFromDiskWithItsOwnName(t *testing.T) {
	a, engine := namedEngine(t, true)
	// The crossing and the one automatic repeat both find the link dead.
	engine.lost = 2
	clickRail(t, a, 0)
	deliver(t, a, typeSteer(t, a, "make it CSV"))

	held := a.unsureSteer()
	if held == nil {
		t.Fatal("a crossing nobody answered is not being held")
	}
	name := held.from
	a.writeDraftsNow(a.leavingDraft())

	next := reopen(t, a)
	next.restoreSentDrafts()
	clickRail(t, next, 0)

	back := next.unsureSteer()
	if back == nil {
		t.Fatal("the correction nobody answered did not survive the window")
	}
	if back.from.Scope != name.Scope || back.from.Seq != name.Seq || !back.from.At.Equal(name.At) {
		t.Fatalf("it came back as %+v, want the name it was sent under (%+v)", back.from, name)
	}
	if back.from.Conversation != next.file {
		t.Fatalf("it came back bound to %q, want this conversation", back.from.Conversation)
	}
	if box := next.input.String(); box != "" {
		t.Fatalf("an unresolved correction was handed back as a draft (%q), where the next enter would rename it", box)
	}
	if rows := steerRows(next); len(rows) != 1 || !rows[0].stalled {
		t.Fatalf("the page draws %+v, want the unresolved correction on it", rows)
	}
	// AND WALKING OUT AND BACK IN DOES NOT MAKE A SECOND ONE. The record is read
	// on the way in and the page is rebuilt on every opening.
	next.closeRoom()
	next.restoreSentDrafts()
	clickRail(t, next, 0)
	if rows := steerRows(next); len(rows) != 1 {
		t.Fatalf("the page draws %+v after a second opening, want one correction", rows)
	}
}

// AND A SEND WHOSE OUTCOME IS UNKNOWN IS NEVER OFFERED AS A DRAFT. Only words
// the engine definitely refused are the person's again.
func TestASendWhoseOutcomeIsUnknownIsNeverOfferedAsADraft(t *testing.T) {
	a, _ := namedEngine(t, true)
	said := a.now()
	for _, state := range []string{draftSendCrossing, draftSendUnanswered, draftSendUndelivered} {
		a.keepSend(taskRecipient(7), outboxSnapshot{
			scope: "yesterday", seq: 4, state: state, at: said,
			line: "make it CSV", words: "make it CSV",
		})
		if _, offered := a.takeRecovered(taskRecipient(7)); offered {
			t.Fatalf("a send written down as %q was offered back as a draft", state)
		}
	}
	// The one that is: the engine read it and said no.
	a.keepSend(taskRecipient(7), outboxSnapshot{
		scope: "yesterday", seq: 5, state: draftSendRefused, at: said,
		line: "make it CSV", words: "make it CSV",
	})
	if _, offered := a.takeRecovered(taskRecipient(7)); !offered {
		t.Fatal("words the engine refused were not the person's again")
	}
}

// AN ANSWER SETTLES THE CONVERSATION THE CORRECTION WAS TYPED IN, even when the
// person has put that whole conversation down and taken up another. Settling the
// composer in front would clear a send belonging to somebody else's page and
// leave this one on disk for ever.
func TestAnAnswerSettlesTheConversationItWasTypedInAfterASwitch(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	crossing := typeSteer(t, a, "make it CSV")

	if sends := keptSendsFor(t, a, taskRecipient(7)); len(sends) != 1 {
		t.Fatalf("the record holds %+v before the switch, want the crossing", sends)
	}

	// The whole conversation goes into the keeper and another takes the surface.
	a.stirs = make(chan behindStirMsg, stirDepth)
	leaving := a.front()
	a.stow(leaving, a.detachConversation())
	a.file = filepath.Join(filepath.Dir(leaving.SessionFile), "another.jsonl")

	deliver(t, a, crossing)

	if got := engine.delivered(); len(got) != 1 {
		t.Fatalf("the task received %v, want the one correction", got)
	}
	held := a.behind[a.convKey(leaving.SessionFile)]
	if held == nil || held.side == nil {
		t.Fatal("the conversation that was put down is not in the keeper")
	}
	if sends := held.side.composers[taskRecipient(7)].sends; len(sends) != 0 {
		t.Fatalf("the answered send is still held by the conversation it was typed in: %+v", sends)
	}
	// AND ITS RECORD LET GO OF IT TOO, written under that conversation's own name
	// while nobody was looking at it.
	if sends := keptSendsFor(t, a, taskRecipient(7)); len(sends) != 0 {
		t.Fatalf("the record still holds a send the engine answered: %+v", sends)
	}
	// AND THE CONVERSATION IN FRONT NEVER HEARD ABOUT IT.
	if state := a.composerAt(taskRecipient(7)); len(state.sends) != 0 || len(state.box.value) != 0 {
		t.Fatalf("the answer landed on the conversation in front: %+v", state)
	}
}

// A READ-ONLY GUEST PAGE HAS NO SENDER AT ALL. Its rows come from another
// conversation's journal, so its `task 7` is not this engine's task 7 — and a
// correction sent from it would reach a stranger's node with the same number.
func TestAGuestPageHasNoSender(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	a.retargetComposer(guestRecipient(filepath.Join(filepath.Dir(a.file), "somebody-else.jsonl"), 7))

	if a.canSteerTask() {
		t.Fatal("a read-only guest page had an ear to send into")
	}
	a.input.setText("make it CSV")
	if cmd := a.steer(); cmd != nil {
		runCmd(cmd)
	}
	if len(engine.asked()) != 0 {
		t.Fatalf("a guest page crossed to this window's engine: %+v", engine.asked())
	}
	if got := a.input.String(); got != "make it CSV" {
		t.Fatalf("a guest page took the words away: %q", got)
	}
	if !strings.Contains(roomText(a), steerGuestWord) {
		t.Fatalf("nothing was said about a page that cannot be corrected from here:\n%s", roomText(a))
	}
}

// AND A CONVERSATION WITH NOWHERE TO KEEP A CORRECTION DOES NOT SEND ONE. The
// send could never be written down first, so it could never be asked about
// again — and an unsafe repeat is worse than a refusal the person can read.
func TestAConversationThatCannotKeepACorrectionDoesNotSendOne(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	a.file = ""

	a.input.setText("make it CSV")
	if cmd := a.steer(); cmd != nil {
		runCmd(cmd)
	}
	if len(engine.asked()) != 0 {
		t.Fatalf("a correction crossed from a conversation with nowhere to keep it: %+v", engine.asked())
	}
	if got := a.input.String(); got != "make it CSV" {
		t.Fatalf("the words were taken away: %q", got)
	}
	if !strings.Contains(roomText(a), steerUnnamedWord) {
		t.Fatalf("nothing was said about why the correction was refused:\n%s", roomText(a))
	}
}

// AND FILES IN THE TRAY ARE SAID RATHER THAN PRETENDED. A correction carries
// words; the tray belongs to the conversation, and it is still there afterwards.
func TestACorrectionSaysTheTrayDoesNotGoWithIt(t *testing.T) {
	a, _ := namedEngine(t, true)
	clickRail(t, a, 0)
	a.attach(picture(t, "chart.png"))
	if len(a.chips) != 1 {
		t.Fatalf("the fixture attached %d files", len(a.chips))
	}

	a.input.setText("make it CSV")
	if cmd := a.steer(); cmd == nil {
		t.Fatal("enter in a task's page sent nothing at all")
	}

	if !strings.Contains(roomText(a), roomTraySteerWord) {
		t.Fatalf("the page said nothing about the files it did not send:\n%s", roomText(a))
	}
	if len(a.chips) != 1 {
		t.Fatalf("the correction spent the conversation's tray: %+v", a.chips)
	}
}

// A CORRECTION MADE AT A TIME IS WRITTEN DOWN AT THAT TIME. The instant is what
// a node's record orders corrections by, so a restored send that took a fresh
// one could be read as arriving after work it was actually made before.
func TestAKeptSendRemembersWhenItWasMade(t *testing.T) {
	a, engine := namedEngine(t, true)
	engine.lost = 2
	clickRail(t, a, 0)
	made := a.now()
	deliver(t, a, typeSteer(t, a, "make it CSV"))
	a.writeDraftsNow(a.leavingDraft())

	next := reopen(t, a)
	next.restoreSentDrafts()

	kept := next.steerSnapshots(next.file, 7)
	if len(kept) != 1 {
		t.Fatalf("the record kept %+v, want the one send nobody answered", kept)
	}
	if !kept[0].at.Equal(made) {
		t.Fatalf("the send came back dated %v, want the instant it was made (%v)", kept[0].at, made)
	}
}

// ── THE DISK IS SLOW, AND THE KEYBOARD IS NOT BEHIND IT ─────────────────────

// A SLOW WRITE DOES NOT TAKE THE KEYBOARD. The record's writer is held open here
// on purpose: a second correction must still be typed, taken and numbered while
// the first one's write is stuck on the disk, and both must cross in the order
// they were typed once it lets go.
func TestASlowRecordWriteDoesNotTakeTheKeyboard(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)

	// The disk is busy: this is the one writer for this record, and nothing can
	// write it until the test lets go.
	writer := draftWriter(draftKeepPath(a.draftFile))
	writer.Lock()

	a.input.setText("the config lives under etc/")
	if cmd := a.steer(); cmd == nil {
		t.Fatal("the first enter sent nothing at all")
	}
	// The write is taken here, on this goroutine, and run on another: nothing but
	// the write itself is allowed to happen off the loop.
	first := waitingWrite(t, a, 1)
	held := make(chan tea.Msg, 1)
	go func() { held <- first() }()

	// The surface goes on answering: a second correction is typed, taken and
	// numbered while that write is still stuck.
	typed := make(chan tea.Cmd, 1)
	go func() {
		a.input.setText("and sort it by date")
		typed <- a.steer()
	}()
	select {
	case second := <-typed:
		if second == nil {
			t.Fatal("the second enter sent nothing at all")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a second enter waited on a disk write — the loop is behind the writer")
	}
	if len(engine.asked()) != 0 {
		t.Fatalf("something crossed while its record was still unwritten: %+v", engine.asked())
	}
	second := waitingWrite(t, a, 2)

	writer.Unlock()

	// The first save was overtaken while it waited, so it writes nothing and its
	// send goes on waiting rather than being released on somebody else's write.
	overtaken, ok := (<-held).(draftKeptMsg)
	if !ok || !errors.Is(overtaken.err, errDraftSuperseded) {
		t.Fatalf("the overtaken save reported %+v, want the newer one to have retired it", overtaken)
	}
	drive(t, a, overtaken)
	if len(engine.asked()) != 0 {
		t.Fatalf("a superseded save released a send: %+v", engine.asked())
	}

	// The newer save carries both, so both cross, in the order they were typed.
	drive(t, a, second())
	got := engine.delivered()
	if len(got) != 2 || got[0].text != "the config lives under etc/" || got[1].text != "and sort it by date" {
		t.Fatalf("the task received %v, want both corrections in the order they were typed", got)
	}
}

// A NEWER RECORD THAT DOES NOT CARRY THE CORRECTION RELEASES NOTHING. "Some save
// after mine landed" is true of a save that CLEARED the record — a close, a
// prune, a box emptied elsewhere — and a send released on that would be a
// correction sent on the strength of words that are on no disk anywhere.
func TestAClearedRecordDoesNotReleaseAnUnwrittenSend(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	path := draftKeepPath(a.draftFile)

	a.input.setText("make it CSV")
	cmd := a.steer()
	if cmd == nil {
		t.Fatal("enter in a task's page sent nothing at all")
	}

	// A newer save reaches the disk first, and it carries no correction at all.
	if err := commitKeep(path, draftKeep{Version: draftKeepVersion, Owner: a.draftKeepOwner()}); err != nil {
		t.Fatalf("writing the cleared record: %v", err)
	}

	drive(t, a, draftKeptFrom(t, drainSave(cmd)))
	if len(engine.asked()) != 0 {
		t.Fatalf("a record that never carried the correction released it: %+v", engine.asked())
	}
	if len(a.outbox.waiting) != 1 {
		t.Fatalf("the send is neither waiting nor refused: %d waiting", len(a.outbox.waiting))
	}

	// AND IT IS NOT STRANDED. The ordinary save that does carry it is what lets it
	// go, under the name it was made with.
	drive(t, a, draftKeptFrom(t, drainSave(a.keepDrafts())))
	if got := engine.delivered(); len(got) != 1 || got[0].text != "make it CSV" {
		t.Fatalf("the task received %v, want the correction once a record carrying it had landed", got)
	}
	if len(a.outbox.waiting) != 0 {
		t.Fatalf("%d sends are still waiting on a write", len(a.outbox.waiting))
	}
}

// AND A CONVERSATION CLOSED WHILE A CORRECTION WAS BEING WRITTEN DOWN SENDS
// NOTHING. The write lands afterwards; there is nobody left for those words.
func TestAClosedConversationDoesNotSendItsWaitingCorrection(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)

	a.input.setText("make it CSV")
	cmd := a.steer()
	if cmd == nil {
		t.Fatal("enter in a task's page sent nothing at all")
	}
	a.forgetSteerOwner(a.steerOwner())

	drive(t, a, draftKeptFrom(t, drainSave(cmd)))
	if len(engine.asked()) != 0 {
		t.Fatalf("a correction crossed into a conversation that had been closed: %+v", engine.asked())
	}
	if len(a.outbox.waiting) != 0 {
		t.Fatalf("a closed conversation is still holding %d sends", len(a.outbox.waiting))
	}
}

// THE RECORD IS THE PROMISE AND THE PLAIN EXPORT IS A CONVENIENCE. A save whose
// record landed and whose export failed has kept the correction, so it crosses —
// and the export's own failure is still said out loud.
func TestAFailedExportDoesNotHoldBackASavedCorrection(t *testing.T) {
	a, engine := namedEngine(t, true)
	// The conversation's own sentence, so the export is a write rather than a
	// remove, and a directory in its place is what fails it.
	typeInto(t, a, "a sentence for the model")
	clickRail(t, a, 0)
	_ = os.Remove(a.draftFile)
	if err := os.Mkdir(a.draftFile, 0o700); err != nil {
		t.Fatal(err)
	}

	a.input.setText("make it CSV")
	cmd := a.steer()
	if cmd == nil {
		t.Fatal("enter in a task's page sent nothing at all")
	}
	kept := draftKeptFrom(t, drainSave(cmd))
	if kept.err == nil {
		t.Fatal("the fixture's export was expected to fail")
	}

	drive(t, a, kept)
	if got := engine.delivered(); len(got) != 1 || got[0].text != "make it CSV" {
		t.Fatalf("the task received %v — a correction whose record landed was refused for the export", got)
	}
	if !noted(a, draftKeepFailWord) {
		t.Fatal("the export that failed was swallowed")
	}
}

// A CORRECTION THIS MACHINE CANNOT NAME IS NOT SENT. Without the bytes to mint an
// identity there is nothing to write down and nothing to ask about later, and
// sending anyway would buy one delivery with the guarantee against a second one.
func TestACorrectionThatCannotBeNamedIsNotSent(t *testing.T) {
	a, engine := namedEngine(t, true)
	clickRail(t, a, 0)
	a.outbox.ready()
	a.outbox.denied = true

	a.input.setText("make it CSV")
	if cmd := a.steer(); cmd != nil {
		runCmd(cmd)
	}

	if len(engine.asked()) != 0 {
		t.Fatalf("a nameless correction crossed: %+v", engine.asked())
	}
	if got := a.input.String(); got != "make it CSV" {
		t.Fatalf("the box holds %q, want the words back on the page they were typed on", got)
	}
	if !noted(a, steerNamelessWord) {
		t.Fatal("nothing was said about the correction that could not be named")
	}
	if rows := steerRows(a); len(rows) != 0 {
		t.Fatalf("the page still draws a correction that never left: %+v", rows)
	}
}
