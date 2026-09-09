package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/history"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE BOX BELONGS TO WHOEVER IT IS TALKING TO (recipient.go).
//
// The defect these pin is one gesture long and it was silent: type a message for
// the model, click a task's row, press enter, and the task received the sentence.
// Nothing on the screen was wrong at any point — the box never changed, so the
// words never changed — and the only thing that moved was where enter pointed.
//
// So every test in this file asks one question in a different place: does an
// unsent sentence stay with the reader it was written for.

// draftLab is a window with a conversation, a running node and a draft file, so
// that a test can ask both halves of "whose words are these" — what is in the box
// and what is on disk.
func draftLab(t *testing.T) (*app, *roomFake) {
	t.Helper()
	a, agent, _ := roomApp(t)
	a.draftFile = filepath.Join(t.TempDir(), "draft.txt")
	return a, agent
}

// savedDraft is what the conversation's draft file holds right now, through the
// same debounce the surface writes it with.
func savedDraft(t *testing.T, a *app) string {
	t.Helper()
	if cmd := a.saveDraft(a.draftFile); cmd != nil {
		cmd()
	}
	return readDraft(a.draftFile)
}

// THE REPORTED DEFECT, IN THE FIXTURE IT WAS REPORTED IN: a real wire client with
// no host label, the roster's own row, and the raw enter that follows the click.
func TestAnUnsentConversationLineIsNeverSentAsTaskSteering(t *testing.T) {
	a, engine := localTaskRoomLab(t)
	const mine = "Unsent discussion for the main conversation"
	a.input.setText(mine)
	a.input.cursor = len("Unsent")

	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatalf("the task click did not open the page:\n%s", taskText(a))
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the task's page opened holding the conversation's words: %q", got)
	}

	drive(t, a, key("enter"))
	engine.mu.Lock()
	steered := append([]string(nil), engine.steered...)
	ids := append([]uint64(nil), engine.ids...)
	engine.mu.Unlock()
	if len(steered) != 0 {
		t.Fatalf("the conversation's draft was sent to task %v as %q", ids, steered)
	}

	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("escape did not return to the conversation")
	}
	if got := a.input.String(); got != mine {
		t.Fatalf("escape came back to %q, want the sentence that was never sent", got)
	}
	if a.input.cursor != len("Unsent") {
		t.Fatalf("the caret came back at %d, want %d", a.input.cursor, len("Unsent"))
	}
}

// EACH PAGE OWNS ITS OWN LINE, INCLUDING ON A DIRECT TASK-TO-TASK SWITCH — which
// is the navigation with no conversation in between, and therefore the one where
// nothing else could have cleared the box.
func TestEachTaskPageOwnsItsOwnUnsentLine(t *testing.T) {
	a, _ := draftLab(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Port the loader",
		session.TaskRunning, session.TaskNotice{})})

	typeInto(t, a, "for the model")

	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "" {
		t.Fatalf("task 7's page opened holding %q", got)
	}
	typeInto(t, a, "for seven")

	a.openRoom(8, "Port the loader")
	if got := a.input.String(); got != "" {
		t.Fatalf("task 8's page opened holding task 7's line: %q", got)
	}
	typeInto(t, a, "for eight")

	a.closeRoom()
	if got := a.input.String(); got != "for the model" {
		t.Fatalf("the conversation came back holding %q", got)
	}

	// And back again, in both directions, with nothing shared.
	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "for seven" {
		t.Fatalf("task 7's page came back holding %q", got)
	}
	a.openRoom(8, "Port the loader")
	if got := a.input.String(); got != "for eight" {
		t.Fatalf("task 8's page came back holding %q", got)
	}
	a.closeRoom()
	if got := a.input.String(); got != "for the model" {
		t.Fatalf("the conversation came back holding %q the second time", got)
	}
}

// REPEATED NAVIGATION KEEPS EVERY LINE WHERE IT WAS TYPED. One round trip can be
// right by accident — a stale copy left in the box reads the same as a restored
// one — so this walks the same three readers several times and edits each of them
// on the way past.
func TestRepeatedNavigationKeepsEveryLineWhereItWasTyped(t *testing.T) {
	a, _ := draftLab(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Port the loader",
		session.TaskRunning, session.TaskNotice{})})

	typeInto(t, a, "m")
	for range 4 {
		a.openRoom(7, "Fix the nil-map crash")
		typeInto(t, a, "7")
		a.openRoom(8, "Port the loader")
		typeInto(t, a, "8")
		a.closeRoom()
		typeInto(t, a, "m")
	}
	if got := a.input.String(); got != "mmmmm" {
		t.Fatalf("the conversation's line grew as %q, want five of its own characters", got)
	}
	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "7777" {
		t.Fatalf("task 7's line grew as %q", got)
	}
	a.openRoom(8, "Port the loader")
	if got := a.input.String(); got != "8888" {
		t.Fatalf("task 8's line grew as %q", got)
	}
}

// AN EMPTY PAGE DRAFT IS NOT A DRAFT. Walking through work without typing must
// not leave a slot per task behind it.
func TestVisitingAPageWithoutTypingKeepsNothing(t *testing.T) {
	a, _ := draftLab(t)
	typeInto(t, a, "for the model")
	for _, id := range []uint64{7, 8, 9} {
		a.openRoom(id, "a node")
		a.closeRoom()
	}
	if len(a.composers) != 0 {
		t.Fatalf("looking at three pages kept %d drafts: %+v", len(a.composers), a.composers)
	}
	if got := a.input.String(); got != "for the model" {
		t.Fatalf("the conversation's own line came back as %q", got)
	}
	// And the conversation's own box is never stashed under a page either.
	a.openRoom(7, "a node")
	if _, kept := a.composers[mainRecipient]; !kept {
		t.Fatal("the conversation's sentence was not kept while its page was open")
	}
	main := a.composers[mainRecipient]
	if got := main.box.String(); got != "for the model" {
		t.Fatalf("the conversation's sentence was kept as %q", got)
	}
}

// THE CONVERSATION'S DRAFT FILE IS THE CONVERSATION'S. A line typed at a task
// must not be what the next launch restores into a box pointed at the model —
// which is the same defect as the misdelivery, told a day later.
func TestTheConversationsDraftFileNeverKeepsALineTypedAtATask(t *testing.T) {
	a, _ := draftLab(t)
	typeInto(t, a, "half a sentence for the model")
	if got := savedDraft(t, a); got != "half a sentence for the model" {
		t.Fatalf("the conversation's own draft was saved as %q", got)
	}

	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "check the loader instead")
	if got := savedDraft(t, a); got != "half a sentence for the model" {
		t.Fatalf("the draft file kept a line typed at a task: %q", got)
	}
	// The same fact asked the way the door out of the program asks it
	// (quitarm.go's [app.leavingDraft], which is what quit writes).
	if got := a.leavingDraft(); got != "half a sentence for the model" {
		t.Fatalf("quitting from a task's page would keep %q as the conversation's draft", got)
	}

	a.closeRoom()
	if got := a.input.String(); got != "half a sentence for the model" {
		t.Fatalf("the box came back holding %q", got)
	}
}

// SUBMISSION SPENDS ONE DRAFT: THE ONE THAT WAS ACCEPTED. Steering a task clears
// that task's line and leaves every other reader's alone, on the screen and on
// disk.
func TestSteeringATaskSpendsOnlyThatPagesLine(t *testing.T) {
	a, agent := draftLab(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Port the loader",
		session.TaskRunning, session.TaskNotice{})})
	typeInto(t, a, "for the model")
	a.openRoom(8, "Port the loader")
	typeInto(t, a, "for eight")
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "keep the API")
	drive(t, a, key("enter"))

	if len(agent.steered) != 1 || agent.steered[0].id != 7 || agent.steered[0].text != "keep the API" {
		t.Fatalf("the node was steered with %+v", agent.steered)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the page that sent kept its own line: %q", got)
	}
	a.closeRoom()
	if got := a.input.String(); got != "for the model" {
		t.Fatalf("a steer into a task spent the conversation's line, leaving %q", got)
	}
	if got := savedDraft(t, a); got != "for the model" {
		t.Fatalf("a steer into a task left the conversation's draft file holding %q", got)
	}
	a.openRoom(8, "Port the loader")
	if got := a.input.String(); got != "for eight" {
		t.Fatalf("a steer into task 7 spent task 8's line, leaving %q", got)
	}
}

// A REFUSED MESSAGE IS THE CONVERSATION'S HOWEVER FAR THE PERSON HAS NAVIGATED.
// The refusal can take a second to arrive — over a connection it is the whole
// upload — and a task page opened in that second owns the box.
func TestARefusedMessageHandsItsPicturesBackToTheConversation(t *testing.T) {
	a, _ := draftLab(t)
	// The tray as [app.submitImagesShown] leaves it while the message is in
	// flight: emptied, and remembered.
	a.sent = []chip{{path: "/tmp/shot.png"}}

	a.openRoom(7, "Fix the nil-map crash")
	a.chipsSettled(errors.New("submit failed"))
	if len(a.chips) != 0 {
		t.Fatalf("the refusal put the conversation's pictures on a task's tray: %+v", a.chips)
	}
	a.closeRoom()
	if len(a.chips) != 1 || a.chips[0].path != "/tmp/shot.png" {
		t.Fatalf("the refusal did not hand the pictures back to the conversation: %+v", a.chips)
	}
}

// A COMPACT PASTE IS PART OF THE LINE IT WAS PASTED INTO, so it is stashed and
// laid back out with it — and the page the person is standing in sends its own
// words and nobody else's documents.
func TestACompactPasteFollowsTheLineItWasPastedInto(t *testing.T) {
	a, agent := draftLab(t)
	if !a.pasteText("alpha\nbeta\ngamma") {
		t.Fatal("the fixture's paste was not held as a chip")
	}
	if !strings.Contains(a.input.String(), pasteTokenHead) {
		t.Fatalf("the conversation's box has no paste token: %q", a.input.String())
	}

	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "" {
		t.Fatalf("the task's page opened holding the conversation's paste: %q", got)
	}
	if len(a.pastes) != 0 {
		t.Fatalf("the task's page opened holding %d of the conversation's documents", len(a.pastes))
	}
	typeInto(t, a, "look at the loader")
	drive(t, a, key("enter"))
	if len(agent.steered) != 1 || agent.steered[0].text != "look at the loader" {
		t.Fatalf("the worker was steered with %+v, want this page's words alone", agent.steered)
	}

	a.closeRoom()
	if !strings.Contains(a.input.String(), pasteTokenHead) {
		t.Fatalf("the conversation's paste token did not come back: %q", a.input.String())
	}
	if len(a.pastes) != 1 || !strings.Contains(a.pastesUnfolded(a.input.String()), "beta") {
		t.Fatalf("the document behind the token was lost: %+v", a.pastes)
	}
}

// THE GUARD'S `m` SPENDS THE PAGE'S LINE ON THE CONVERSATION, and the
// conversation's own unsent sentence is not what it spends.
func TestTheGuardSendsThePagesLineAndKeepsTheConversations(t *testing.T) {
	a, agent := draftLab(t)
	agent.steerErr = errors.New("task 7 is done, not running")
	typeInto(t, a, "for the model")

	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "check the other directory")
	drive(t, a, key("enter"))
	if !a.guarding() {
		t.Fatalf("a refused steer did not raise the guard:\n%s", roomText(a))
	}
	drive(t, a, key("m"))

	if len(agent.sent) != 1 || agent.sent[0] != "check the other directory" {
		t.Fatalf("[m] sent %+v, want the page's own sentence", agent.sent)
	}
	if a.roomOpen() {
		t.Fatal("[m] left the page open under a message that went to the model")
	}
	if got := a.input.String(); got != "for the model" {
		t.Fatalf("[m] spent the conversation's own line, leaving %q", got)
	}
	if got := savedDraft(t, a); got != "for the model" {
		t.Fatalf("[m] left the conversation's draft file holding %q", got)
	}
}

// A HISTORY WALK IS A MODE, AND IT DOES NOT FOLLOW YOU TO ANOTHER PAGE. The walk
// holds the draft it interrupted; carried across, the esc that ends it would put
// one reader's sentence into another's box.
func TestAHistoryWalkDoesNotCarryALineToAnotherPage(t *testing.T) {
	a, _ := draftLab(t)
	store := history.New(filepath.Join(t.TempDir(), "history.jsonl"))
	t.Cleanup(func() { _ = store.Close() })
	store.Append("an old prompt", "/tmp/lab")
	a.history, a.workspace = store, "/tmp/lab"

	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "mine")
	drive(t, a, key("up"))
	if a.input.String() != "an old prompt" || !a.recalling() {
		t.Fatalf("the walk did not start: %q (recalling=%v)", a.input.String(), a.recalling())
	}

	a.openRoom(8, "Port the loader")
	if a.recalling() {
		t.Fatal("the history walk followed the person to another page")
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the second page opened holding %q", got)
	}
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("escape did not leave the second page")
	}
	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "mine" {
		t.Fatalf("the walk kept the first page's own sentence as %q", got)
	}
}

// A CONVERSATION PUT DOWN AND PICKED UP AGAIN BRINGS ITS PAGES' LINES WITH IT,
// and the sentence the person is carrying is still the conversation's own.
func TestAKeptConversationBringsItsPagesLinesBack(t *testing.T) {
	a, _ := draftLab(t)
	typeInto(t, a, "for the model")
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "for seven")
	a.closeRoom()

	side := a.detachConversation()
	if side.draft != "for the model" {
		t.Fatalf("the conversation went into the sidecar as %q", side.draft)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the surface kept %q after putting the conversation down", got)
	}
	if len(a.composers) != 0 {
		t.Fatalf("the surface kept %d page drafts of a conversation it put down", len(a.composers))
	}

	a.restoreAside(side)
	if got := a.input.String(); got != "for the model" {
		t.Fatalf("the conversation came back holding %q", got)
	}
	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "for seven" {
		t.Fatalf("the task's page came back holding %q", got)
	}
}

// AND A CONVERSATION THAT IS REPLACED TAKES ITS PAGES' LINES WITH IT. A task id
// means something only inside the graph that minted it, so a line typed at task 7
// may not appear in the next conversation's task 7.
func TestANewConversationDoesNotInheritThePagesLines(t *testing.T) {
	a, _ := draftLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "for seven")
	a.closeRoom()

	_ = a.detachConversation()
	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "" {
		t.Fatalf("a task page in the next conversation opened holding %q", got)
	}
}

// THE DRAFT FILE ROUND TRIP, END TO END: what one window writes on the way out is
// what the next window opens with, and a line typed at a task is not in it.
func TestTheRestoredDraftIsTheConversationsAndNotAPages(t *testing.T) {
	a, _ := draftLab(t)
	typeInto(t, a, "the sentence I was writing")
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "a correction for the worker")
	// The window goes away with the page still open, which is the whole of what
	// makes this different from the ordinary case.
	writeDraft(a.draftFile, a.leavingDraft())

	next, _, _ := roomApp(t)
	next.draftFile = a.draftFile
	next.workspace = "/tmp/lab"
	next.restoreDraft()
	if got := next.input.String(); got != "the sentence I was writing" {
		t.Fatalf("the next window opened holding %q", got)
	}
	if _, err := os.Stat(next.draftFile); err != nil {
		t.Fatalf("the restored draft file went missing: %v", err)
	}
}
