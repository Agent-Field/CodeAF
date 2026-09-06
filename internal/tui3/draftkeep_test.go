package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// AN UNSENT LINE OUTLIVES THE WINDOW (draftkeep.go).
//
// recipientdraft_test.go pins where a sentence goes while the window is open.
// These pin the other half: that it is still there when the window is not, that
// a restart puts each recipient's words back in front of the recipient they were
// written for, and that nothing is lost quietly when a write cannot happen.

// keepLab is a window with a conversation on disk, a running node and a draft
// file of its own, which is the least a durability test can be about.
func keepLab(t *testing.T) (*app, string) {
	t.Helper()
	a, _, _ := roomApp(t)
	dir := t.TempDir()
	a.workspace = "/tmp/lab"
	a.file = filepath.Join(dir, "s1.jsonl")
	a.draftFile = DraftFile(dir, a.workspace)
	return a, dir
}

// reopen is the next window on the same conversation: a fresh surface handed the
// same draft file and the same transcript, restoring exactly as a launch does.
func reopen(t *testing.T, was *app) *app {
	t.Helper()
	next, _, _ := roomApp(t)
	next.workspace, next.file, next.draftFile = was.workspace, was.file, was.draftFile
	next.host = was.host
	next.restoreDraft()
	return next
}

// picture is a real file on disk, because a tray chip names one.
func picture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// noted reports whether the surface said something with this phrase in it.
func noted(a *app, phrase string) bool {
	for _, held := range a.entries {
		if held.kind == entryNote && strings.Contains(held.text, phrase) {
			return true
		}
	}
	return false
}

// keptSlotFor is one recipient's slot in a record on disk, for a test that cares
// what was left behind rather than what was laid out.
func keptSlotFor(t *testing.T, path string, who recipient) (draftKeepSlot, bool) {
	t.Helper()
	keep, how := readDraftKeep(path)
	if how != draftKeepFound {
		return draftKeepSlot{}, false
	}
	for _, slot := range keep.Slots {
		if slot.Task == who.task && slot.Run == who.run {
			return slot, true
		}
	}
	return draftKeepSlot{}, false
}

// THE WHOLE COMPOSER COMES BACK, for every recipient: the words, the caret, the
// documents behind the compact tags and the tray.
func TestTheWholeComposerComesBackAfterTheWindowGoes(t *testing.T) {
	a, _ := keepLab(t)
	shot := picture(t, "chart.png")

	typeInto(t, a, "have a look at this")
	if !a.pasteText("alpha\nbeta\ngamma") {
		t.Fatal("the fixture's paste was not held as a chip")
	}
	a.attach(shot)
	a.input.cursor = len("have a look")
	mainLine, mainCaret := a.input.String(), a.input.cursor

	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "keep the API compatible")
	if !a.pasteText("one\ntwo\nthree") {
		t.Fatal("the room's paste was not held as a chip")
	}
	roomLine := a.input.String()
	a.closeRoom()

	// The door out of the program, which writes everything down before it goes.
	a.writeDraftsNow(a.leavingDraft())

	next := reopen(t, a)
	if got := next.input.String(); got != mainLine {
		t.Fatalf("the conversation came back holding %q, want %q", got, mainLine)
	}
	if next.input.cursor != mainCaret {
		t.Fatalf("the caret came back at %d, want %d", next.input.cursor, mainCaret)
	}
	if len(next.pastes) != 1 || !strings.Contains(next.pastesUnfolded(next.input.String()), "beta") {
		t.Fatalf("the document behind the conversation's tag was lost: %+v", next.pastes)
	}
	if len(next.chips) != 1 || next.chips[0].path != shot {
		t.Fatalf("the tray came back as %+v", next.chips)
	}

	next.openRoom(7, "Fix the nil-map crash")
	if got := next.input.String(); got != roomLine {
		t.Fatalf("the task's page came back holding %q, want %q", got, roomLine)
	}
	if len(next.pastes) != 1 || !strings.Contains(next.pastesUnfolded(next.input.String()), "two") {
		t.Fatalf("the document behind the task page's tag was lost: %+v", next.pastes)
	}
}

// AND THE PLAIN FILE BESIDE THE RECORD STILL HOLDS THE CONVERSATION'S WORDS,
// which is the whole of the migration: a build without any of this reads and
// writes that file exactly as it always did.
func TestThePlainFileStillHoldsTheConversationsWords(t *testing.T) {
	a, _ := keepLab(t)
	typeInto(t, a, "the sentence I was writing")
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "a line for the worker")
	a.closeRoom()
	a.writeDraftsNow(a.leavingDraft())

	if got := readDraft(a.draftFile); got != "the sentence I was writing" {
		t.Fatalf("the plain file holds %q", got)
	}
}

// AND A LINE TYPED AT A TASK IS NEVER WHAT THE CONVERSATION OPENS WITH. This is
// the misdelivery told one restart later, and the conversation's box having
// nothing in it is the whole of what makes the test honest.
func TestARestoredTaskLineIsNotTheConversationsDraft(t *testing.T) {
	a, _ := keepLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "check the loader instead")
	a.closeRoom()
	a.writeDraftsNow(a.leavingDraft())

	next := reopen(t, a)
	if got := next.input.String(); got != "" {
		t.Fatalf("the conversation opened holding a line typed at a task: %q", got)
	}
	if got := readDraft(next.draftFile); got != "" {
		t.Fatalf("the conversation's own draft file holds %q", got)
	}
	next.openRoom(7, "Fix the nil-map crash")
	if got := next.input.String(); got != "check the loader instead" {
		t.Fatalf("the task's page came back holding %q", got)
	}
}

// ANOTHER CONVERSATION'S LINE IS NEITHER SHOWN NOR THROWN AWAY. A task number
// means something only inside the graph that minted it, so a window opening a
// different conversation under the same name may not lay it out — and may not
// delete it either, because the conversation it belongs to can still be opened.
func TestAnotherConversationsTaskLineIsNeitherShownNorDeleted(t *testing.T) {
	a, _ := keepLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "yesterday's correction")
	a.closeRoom()
	a.writeDraftsNow(a.leavingDraft())

	// A different conversation, in the same window's place.
	other, _, _ := roomApp(t)
	other.workspace, other.draftFile = a.workspace, a.draftFile
	other.file = filepath.Join(filepath.Dir(a.file), "s2.jsonl")
	other.restoreDraft()
	other.openRoom(7, "Fix the nil-map crash")
	if got := other.input.String(); got != "" {
		t.Fatalf("another conversation's task 7 opened holding %q", got)
	}
	other.closeRoom()
	// It writes its own record over the same name, and what it was carrying for
	// somebody else goes back down with it.
	other.writeDraftsNow(other.leavingDraft())

	back := reopen(t, a)
	back.openRoom(7, "Fix the nil-map crash")
	if got := back.input.String(); got != "yesterday's correction" {
		t.Fatalf("the conversation that typed it came back to %q", got)
	}
}

// AND NEITHER IS ONE FROM ANOTHER MACHINE OR ANOTHER PROJECT. A transcript path
// is not an identity: a session opened over a connection names a file on the far
// machine, and the same path reached two ways is two conversations.
func TestATaskLineIsNotRestoredAcrossHostOrProject(t *testing.T) {
	a, _ := keepLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "yesterday's correction")
	a.closeRoom()
	a.writeDraftsNow(a.leavingDraft())

	for _, strange := range []struct {
		what      string
		host      string
		workspace string
	}{
		{what: "another machine", host: "devbox", workspace: a.workspace},
		{what: "another project", host: a.host, workspace: "/tmp/other"},
	} {
		far, _, _ := roomApp(t)
		far.file, far.draftFile = a.file, a.draftFile
		far.host, far.workspace = strange.host, strange.workspace
		far.restoreDraft()
		far.openRoom(7, "Fix the nil-map crash")
		if got := far.input.String(); got != "" {
			t.Fatalf("a conversation on %s opened task 7 holding %q", strange.what, got)
		}
		far.closeRoom()
		far.writeDraftsNow(far.leavingDraft())
	}

	// AND IT IS STILL THERE for the conversation that typed it.
	back := reopen(t, a)
	back.openRoom(7, "Fix the nil-map crash")
	if got := back.input.String(); got != "yesterday's correction" {
		t.Fatalf("the conversation that typed it came back to %q", got)
	}
}

// A TASK PAGE'S LINE FOLLOWS ITS CONVERSATION INTO WHATEVER WINDOW OPENS IT
// NEXT. The record is named after the window, and the window that opens a
// conversation tomorrow is not the one that closed it — so the reunion is by
// conversation and never by file name.
func TestATaskLineFollowsItsConversationIntoAnotherWindow(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s1.jsonl")

	// A window that has since died, with a correction still in one of its pages.
	gone, _, _ := roomApp(t)
	gone.workspace, gone.file = "/tmp/lab", session
	gone.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	gone.openRoom(7, "Fix the nil-map crash")
	typeInto(t, gone, "widen the pipe")
	gone.closeRoom()
	gone.writeDraftsNow(gone.leavingDraft())
	left := draftKeepPath(gone.draftFile)

	// Today's window opens the same conversation under a name of its own.
	next, _, _ := roomApp(t)
	next.workspace, next.file = "/tmp/lab", session
	next.draftFile = DraftFile(dir, "/tmp/lab")
	next.restoreDraft()
	next.openRoom(7, "Fix the nil-map crash")
	if got := next.input.String(); got != "widen the pipe" {
		t.Fatalf("the conversation's own page opened holding %q", got)
	}

	// AND WHAT IT TOOK, IT MOVED: the dead window's record no longer offers the
	// same line to the window after this one.
	if slot, held := keptSlotFor(t, left, taskRecipient(7)); held {
		t.Fatalf("the line was copied rather than moved: %+v", slot)
	}
}

// A SENTENCE ADOPTED FROM A DEAD WINDOW ON THIS DIRECTORY BRINGS NOTHING WITH
// IT. The draft follows the person and the directory; a task page follows its
// conversation, and two directories matching is not that conversation.
func TestAnAdoptedSentenceDoesNotBringAStrangersTaskLine(t *testing.T) {
	dir := t.TempDir()

	gone, _, _ := roomApp(t)
	gone.workspace, gone.file = "/tmp/lab", filepath.Join(dir, "s1.jsonl")
	gone.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	typeInto(t, gone, "yesterday's sentence")
	gone.openRoom(7, "Fix the nil-map crash")
	typeInto(t, gone, "yesterday's correction")
	gone.closeRoom()
	gone.writeDraftsNow(gone.leavingDraft())
	left := draftKeepPath(gone.draftFile)

	// A DIFFERENT conversation opens in this directory and adopts the orphan.
	next, _, _ := roomApp(t)
	next.workspace, next.file = "/tmp/lab", filepath.Join(dir, "s2.jsonl")
	next.draftFile = DraftFile(dir, "/tmp/lab")
	next.restoreDraft()
	if got := next.input.String(); got != "yesterday's sentence" {
		t.Fatalf("the orphan sentence came back as %q", got)
	}
	next.openRoom(7, "Fix the nil-map crash")
	if got := next.input.String(); got != "" {
		t.Fatalf("a stranger's task 7 was laid out in this conversation: %q", got)
	}

	// AND THE STRANGER'S OWN PAGE IS STILL WHERE IT WAS, for the window that
	// opens that conversation next.
	if _, held := keptSlotFor(t, left, taskRecipient(7)); !held {
		t.Fatal("adopting a sentence deleted another conversation's task line")
	}
}

// A COMPACT TAG WITH NO DOCUMENT BEHIND IT KEEPS ITS WORDS AND IS NAMED. It can
// only reach a box from a plain file written by a build that kept no documents,
// and the words are still the person's: the surface says what is missing rather
// than editing their sentence for them.
func TestATagWithNoDocumentKeepsTheWordsAndIsNamed(t *testing.T) {
	a, _ := keepLab(t)
	// Exactly what an older build left behind: the words, and nothing beside them.
	line := "look at " + pasteToken(1, 3) + " and tell me"
	if err := writeDraft(a.draftFile, line); err != nil {
		t.Fatal(err)
	}

	next := reopen(t, a)
	if got := next.input.String(); got != line {
		t.Fatalf("the restored sentence is %q, want %q", got, line)
	}
	if !noted(next, draftLostPasteWord) {
		t.Fatal("nothing was said about the tag with no document behind it")
	}
}

// AND A SENTENCE NOBODY EDITED IS NOT REFORMATTED.
func TestARestoredSentenceKeepsItsOwnSpacing(t *testing.T) {
	a, _ := keepLab(t)
	if err := writeDraft(a.draftFile, "two  spaces  and\n  an indent"); err != nil {
		t.Fatal(err)
	}

	next := reopen(t, a)
	if got := next.input.String(); got != "two  spaces  and\n  an indent" {
		t.Fatalf("the restored sentence was reformatted: %q", got)
	}
}

// AN ATTACHMENT WHOSE FILE HAS GONE IS STILL ON THE TRAY, AND IS NAMED. The
// person put it there; taking it off quietly would send a different message from
// the one they wrote, and `enter` names it again if they send it anyway.
func TestAnAttachmentThatWentAwayIsKeptAndNamed(t *testing.T) {
	a, _ := keepLab(t)
	staying, going := picture(t, "keep.png"), picture(t, "gone.png")
	typeInto(t, a, "look at these")
	a.attach(staying)
	a.attach(going)
	a.writeDraftsNow(a.leavingDraft())
	if err := os.Remove(going); err != nil {
		t.Fatal(err)
	}

	next := reopen(t, a)
	if len(next.chips) != 2 {
		t.Fatalf("the tray came back as %+v", next.chips)
	}
	if !noted(next, "gone.png") {
		t.Fatal("nothing was said about the attachment that is no longer there")
	}
}

// A SUBMIT SPENDS THE CONVERSATION'S SENTENCE AND NOTHING ELSE, on disk as well
// as on screen.
func TestSubmittingTheConversationLeavesThePagesLinesOnDisk(t *testing.T) {
	a, _ := keepLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "for seven")
	a.closeRoom()
	typeInto(t, a, "for the model")
	a.writeDraftsNow(a.leavingDraft())

	// Enter's own door: the words went to the model, so the file goes.
	a.input.reset()
	a.dropDraft()
	if _, err := os.Stat(a.draftFile); !os.IsNotExist(err) {
		t.Fatalf("submit left the conversation's draft file behind (%v)", err)
	}

	next := reopen(t, a)
	if got := next.input.String(); got != "" {
		t.Fatalf("the conversation came back holding a sentence it sent: %q", got)
	}
	next.openRoom(7, "Fix the nil-map crash")
	if got := next.input.String(); got != "for seven" {
		t.Fatalf("submitting in the conversation spent a task page's line: %q", got)
	}
}

// AND A SAVE THAT WAS OVERTAKEN CANNOT PUT THE SENT SENTENCE BACK. The debounce
// hands its write to a command; commands do not arrive in order, so the write
// built before a submit can reach the disk after it.
func TestAnOvertakenSaveCannotResurrectASentSentence(t *testing.T) {
	a, _ := keepLab(t)
	typeInto(t, a, "the sentence I sent")

	// The debounce fires: the write is built here and runs later.
	late := a.keepDrafts()
	if late == nil {
		t.Fatal("the fixture armed no write")
	}

	// Enter lands first.
	a.input.reset()
	a.dropDraft()

	// And the overtaken write arrives afterwards, having written nothing and
	// saying which of the two it was.
	msg, ok := late().(draftKeptMsg)
	if !ok || !errors.Is(msg.err, errDraftSuperseded) {
		t.Fatalf("the late write reported %+v", msg)
	}
	if got := readDraft(a.draftFile); got != "" {
		t.Fatalf("a sent sentence was written back to disk: %q", got)
	}
	next := reopen(t, a)
	if got := next.input.String(); got != "" {
		t.Fatalf("the next window opened holding a sentence that was sent: %q", got)
	}
}

// A RECORD THIS BUILD CANNOT READ IS MOVED ASIDE, NEVER OVERWRITTEN. It is
// somebody's unsent words in a shape from a later build or a half-finished
// write, and replacing it would be deleting them for not understanding them.
func TestAnUnreadableRecordIsKeptBesideTheNewOne(t *testing.T) {
	for _, strange := range []struct {
		what string
		raw  string
	}{
		{what: "half a record", raw: `{"version":1,"slots":[`},
		{what: "a later build's", raw: `{"version":99,"slots":[{"task":7,"text":"from tomorrow"}]}`},
	} {
		t.Run(strange.what, func(t *testing.T) {
			a, _ := keepLab(t)
			path := draftKeepPath(a.draftFile)
			if err := os.WriteFile(path, []byte(strange.raw), 0o600); err != nil {
				t.Fatal(err)
			}

			typeInto(t, a, "today's sentence")
			a.writeDraftsNow(a.leavingDraft())

			aside, err := filepath.Glob(path + ".unreadable-*")
			if err != nil || len(aside) != 1 {
				t.Fatalf("the unreadable record was not kept aside: %v %v", aside, err)
			}
			raw, err := os.ReadFile(aside[0])
			if err != nil || string(raw) != strange.raw {
				t.Fatalf("what was kept aside is %q (%v)", raw, err)
			}
			if _, how := readDraftKeep(path); how != draftKeepFound {
				t.Fatalf("the new record was not written in its place (%v)", how)
			}
			next := reopen(t, a)
			if got := next.input.String(); got != "today's sentence" {
				t.Fatalf("the window after it opened holding %q", got)
			}
		})
	}
}

// A REUNION TAKES NOTHING UNTIL IT HAS SOMEWHERE TO PUT IT. A line is MOVED out
// of a dead window's record, and a move whose second half fails is a deletion —
// so when this window cannot write its own record, everything stays where it is
// and the surface says so.
func TestAReunionKeepsTheOldRecordWhenTheNewOneCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s1.jsonl")

	gone, _, _ := roomApp(t)
	gone.workspace, gone.file = "/tmp/lab", session
	gone.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	gone.openRoom(7, "Fix the nil-map crash")
	typeInto(t, gone, "widen the pipe")
	gone.closeRoom()
	gone.writeDraftsNow(gone.leavingDraft())
	left := draftKeepPath(gone.draftFile)

	next, _, _ := roomApp(t)
	next.workspace, next.file = "/tmp/lab", session
	next.draftFile = DraftFile(dir, "/tmp/lab")
	// A directory at the temporary file path makes the write fail even as root.
	if err := os.Mkdir(draftKeepPath(next.draftFile)+".writing-"+strconv.Itoa(draftOwner), 0o700); err != nil {
		t.Fatal(err)
	}
	next.restoreDraft()

	if !noted(next, draftKeepFailWord) {
		t.Fatal("a draft that could not be written was reported as saved")
	}
	if _, held := keptSlotFor(t, left, taskRecipient(7)); !held {
		t.Fatal("the line was taken out of the old record and never written to a new one")
	}
}

// A CONVERSATION CLOSED FOR REAL TAKES ITS PAGES' LINES WITH IT: its tasks
// closed with it, so there is nobody left for those words to be delivered to.
func TestClosingAConversationDropsItsRecord(t *testing.T) {
	a, _ := keepLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "for seven")
	a.closeRoom()
	a.writeDraftsNow(a.leavingDraft())
	if _, how := readDraftKeep(draftKeepPath(a.draftFile)); how != draftKeepFound {
		t.Fatal("the record was never written")
	}

	dropDraftFile(a.draftFile)
	if _, how := readDraftKeep(draftKeepPath(a.draftFile)); how != draftKeepNone {
		t.Fatal("closing the conversation left its record behind")
	}
}

// AND IT LEAVES BEHIND WHAT IT WAS ONLY CARRYING. The record is named after the
// window, so it can hold a conversation this one has nothing to do with.
func TestClosingAConversationKeepsWhatItWasCarrying(t *testing.T) {
	a, _ := keepLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "yesterday's correction")
	a.closeRoom()
	a.writeDraftsNow(a.leavingDraft())

	// A second conversation under the same window's name, which carries the first
	// one's line without ever showing it.
	other, _, _ := roomApp(t)
	other.workspace, other.draftFile = a.workspace, a.draftFile
	other.file = filepath.Join(filepath.Dir(a.file), "s2.jsonl")
	other.restoreDraft()
	typeInto(t, other, "a sentence of its own")
	other.writeDraftsNow(other.leavingDraft())

	dropDraftFile(other.draftFile)
	if _, held := keptSlotFor(t, draftKeepPath(a.draftFile), taskRecipient(7)); !held {
		t.Fatal("closing one conversation deleted another one's unsent line")
	}
}

// A SAVE THE PERSON HAS ALREADY SUPERSEDED IS RETIRED WHEN THEY SUPERSEDE IT,
// not when the newer one reaches the disk. A newer save that FAILS used to leave
// the older one still allowed to land, which put words the person had cleared
// back on disk looking current.
func TestAFailedNewerSaveRetiresAnOlderQueuedOne(t *testing.T) {
	a, _ := keepLab(t)
	a.input.setText("the last good sentence")
	a.writeDraftsNow(a.leavingDraft())

	// A save built while they were still typing, handed to a command.
	a.input.setText("the older queued sentence")
	old := a.keepDrafts()
	if old == nil {
		t.Fatal("the fixture armed no write")
	}

	// Then they clear the box, and THAT save cannot reach the disk: a directory
	// at the temporary path fails the write for everybody, root included.
	blocked := draftKeepPath(a.draftFile) + ".writing-" + strconv.Itoa(draftOwner)
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	a.input.setText("")
	if msg := a.keepDrafts()(); msg == nil {
		t.Fatal("the newer save was expected to fail")
	}
	// The write tidies its own temporary path on the way out, so this may already
	// be gone.
	_ = os.Remove(blocked)

	// The overtaken save arrives afterwards, writes nothing at all, and SAYS SO:
	// a skipped write reported as a success would be the proof a pending send is
	// released on (steersend.go's [app.sendsKeptAt]).
	msg, ok := old().(draftKeptMsg)
	if !ok || !errors.Is(msg.err, errDraftSuperseded) {
		t.Fatalf("the retired save reported %+v", msg)
	}
	if got := readDraft(a.draftFile); got != "the last good sentence" {
		t.Fatalf("the plain file holds %q", got)
	}
	next := reopen(t, a)
	if got := next.input.String(); got != "the last good sentence" {
		t.Fatalf("the next window opened holding %q", got)
	}
}

// ENTER REFUSES A MESSAGE WHOSE PASTED BLOCK IS MISSING, AND KEEPS EVERY WORD OF
// IT. Sending would hand the model `[paste 1 · 3 lines]` as though those were the
// words; editing the tag out would send a different message from the one on the
// screen. So nothing is sent and nothing is changed.
func TestEnterRefusesAMessageWhosePastedBlockIsMissing(t *testing.T) {
	a, _ := keepLab(t)
	line := "look at " + pasteToken(1, 3) + " and tell me"
	if err := writeDraft(a.draftFile, line); err != nil {
		t.Fatal(err)
	}

	next := reopen(t, a)
	next.enter()
	if got := next.input.String(); got != line {
		t.Fatalf("the refused message left the box holding %q", got)
	}
	if !noted(next, draftOrphanSendWord) {
		t.Fatal("nothing was said about the pasted block that could not be restored")
	}
}

// AND SO DOES A TASK PAGE'S OWN ENTER: a correction is the last message that can
// afford to arrive as half of itself.
func TestSteeringRefusesAMessageWhosePastedBlockIsMissing(t *testing.T) {
	a, agent, _ := roomApp(t)
	a.openRoom(7, "Fix the nil-map crash")
	line := "try " + pasteToken(1, 3) + " instead"
	a.input.setText(line)

	a.steer()
	if len(agent.steered) != 0 {
		t.Fatalf("a message with a missing pasted block was steered: %+v", agent.steered)
	}
	if got := a.input.String(); got != line {
		t.Fatalf("the refused correction left the box holding %q", got)
	}
}

// A BOX THAT WAS CLEARED STAYS CLEARED IN A WINDOW WITH A NAME OF ITS OWN. The
// record is named after the window, so a new process reads the old one through
// the reunion — and the crash that left a stale export behind is exactly the
// case where the older record must not be believed.
func TestAClearedDraftDoesNotReturnInANewWindow(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s1.jsonl")

	gone, _, _ := roomApp(t)
	gone.workspace, gone.file = "/tmp/lab", session
	gone.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	gone.input.setText("already submitted")
	gone.writeDraftsNow(gone.leavingDraft())
	gone.input.setText("")
	// The record is committed and the window dies before its export is rewritten.
	if err := commitKeep(draftKeepPath(gone.draftFile), gone.draftKeepBuild("")); err != nil {
		t.Fatal(err)
	}
	if got := readDraft(gone.draftFile); got != "already submitted" {
		t.Fatalf("the fixture left no stale export: %q", got)
	}

	next, _, _ := roomApp(t)
	next.workspace, next.file = "/tmp/lab", session
	next.draftFile = DraftFile(dir, "/tmp/lab")
	next.restoreDraft()
	if got := next.input.String(); got != "" {
		t.Fatalf("a new window opened holding a sentence that was sent: %q", got)
	}
	// AND THE STALE EXPORT IS GONE, so no later window can adopt it as an orphan.
	if got := readDraft(gone.draftFile); got != "" {
		t.Fatalf("the stale export is still on disk: %q", got)
	}
}

// AND AN OLDER DEAD RECORD DOES NOT UNDO A NEWER CLEAR. Two windows can leave
// two records for one conversation; only the newest is read, so the sentence a
// later window spent is not handed back by an earlier one — and the earlier one
// is left on disk rather than deleted.
func TestAnOlderDeadRecordDoesNotUndoANewerClear(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s1.jsonl")

	// The window that had the sentence.
	first, _, _ := roomApp(t)
	first.workspace, first.file = "/tmp/lab", session
	first.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	first.input.setText("the sentence I sent")
	older := first.draftKeepBuild("the sentence I sent")
	older.At = "2026-09-06T10:00:00Z"
	if err := commitKeep(draftKeepPath(first.draftFile), older); err != nil {
		t.Fatal(err)
	}

	// The window that sent it, an hour later, and died before anything else.
	second, _, _ := roomApp(t)
	second.workspace, second.file = "/tmp/lab", session
	second.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(dir, "/tmp/lab") })
	newer := second.draftKeepBuild("")
	newer.At = "2026-09-06T11:00:00Z"
	if err := commitKeep(draftKeepPath(second.draftFile), newer); err != nil {
		t.Fatal(err)
	}

	next, _, _ := roomApp(t)
	next.workspace, next.file = "/tmp/lab", session
	next.draftFile = DraftFile(dir, "/tmp/lab")
	next.restoreDraft()
	if got := next.input.String(); got != "" {
		t.Fatalf("an older record handed back a sentence that was sent: %q", got)
	}
	if _, held := keptSlotFor(t, draftKeepPath(first.draftFile), mainRecipient); !held {
		t.Fatal("the older record was deleted rather than left where it was")
	}
}

// A PAGE ONTO SOMEBODY ELSE'S WORK KEEPS ITS OWN LINE. `task 7` of another
// conversation is not this conversation's task 7, and the two must not share a
// box on the screen or a slot on disk (recipient.go's [guestRecipient]).
func TestAGuestPagesLineIsNotTheLocalTasks(t *testing.T) {
	a, _ := keepLab(t)
	elsewhere := filepath.Join(filepath.Dir(a.file), "s2.jsonl")

	a.atComposer(taskRecipient(7), func(state *composerState) {
		state.box.setText("for my own task seven")
	})
	a.atComposer(guestRecipient(elsewhere, 7), func(state *composerState) {
		state.box.setText("for the other conversation's task seven")
	})
	a.writeDraftsNow(a.leavingDraft())

	next := reopen(t, a)
	mine := next.composerAt(taskRecipient(7))
	if got := mine.box.String(); got != "for my own task seven" {
		t.Fatalf("this conversation's task 7 came back holding %q", got)
	}
	theirs := next.composerAt(guestRecipient(elsewhere, 7))
	if got := theirs.box.String(); got != "for the other conversation's task seven" {
		t.Fatalf("the guest page came back holding %q", got)
	}
}

// A WINDOW THAT NAMED NO CONVERSATION STILL KEEPS ITS BOX WHOLE — the documents
// and the tray with it — and promises nothing about a page.
func TestAWindowWithNoTranscriptKeepsItsBoxWhole(t *testing.T) {
	a, _ := keepLab(t)
	// A door that handed the surface no transcript path.
	a.file = ""
	shot := picture(t, "chart.png")

	typeInto(t, a, "look at this")
	if !a.pasteText("alpha\nbeta\ngamma") {
		t.Fatal("the fixture's paste was not held as a chip")
	}
	a.attach(shot)
	mainLine := a.input.String()
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "for seven")
	a.closeRoom()
	a.writeDraftsNow(a.leavingDraft())

	next, _, _ := roomApp(t)
	next.workspace, next.draftFile = a.workspace, a.draftFile
	next.restoreDraft()
	if got := next.input.String(); got != mainLine {
		t.Fatalf("the box came back as %q, want %q", got, mainLine)
	}
	if len(next.pastes) != 1 || !strings.Contains(next.pastesUnfolded(next.input.String()), "beta") {
		t.Fatalf("the document behind the tag was lost: %+v", next.pastes)
	}
	if len(next.chips) != 1 || next.chips[0].path != shot {
		t.Fatalf("the tray came back as %+v", next.chips)
	}
	// AND NOTHING IS PROMISED ABOUT A PAGE OF A CONVERSATION THIS WINDOW CANNOT
	// NAME: a task id means something only inside the graph that minted it.
	next.openRoom(7, "Fix the nil-map crash")
	if got := next.input.String(); got != "" {
		t.Fatalf("a page's line was kept under a window's name: %q", got)
	}
}

// ── the outbox seam ─────────────────────────────────────────────────────────

// AN UNCERTAIN MESSAGE IS KEPT AGAINST ITS RECIPIENT, under the name that makes
// asking again the SAME message rather than a second one.
func TestAnUncertainMessageIsKeptAgainstItsRecipient(t *testing.T) {
	a, _ := keepLab(t)
	a.keepSend(taskRecipient(7), outboxSnapshot{
		scope: "abc", seq: 4, line: "try the other loader", words: "try the other loader",
		state: draftSendCrossing, at: a.now(),
	})
	a.writeDraftsNow(a.leavingDraft())

	next := reopen(t, a)
	kept := next.composerAt(taskRecipient(7)).sends
	if len(kept) != 1 || kept[0].scope != "abc" || kept[0].seq != 4 {
		t.Fatalf("the uncertain message came back as %+v", kept)
	}
	if kept[0].line != "try the other loader" {
		t.Fatalf("its words came back as %q", kept[0].line)
	}
	if got := next.input.String(); got != "" {
		t.Fatalf("an uncertain message for a task landed in the conversation: %q", got)
	}
	// And settling it forgets it.
	next.dropSend(taskRecipient(7), "abc", 4)
	if kept := next.composerAt(taskRecipient(7)).sends; len(kept) != 0 {
		t.Fatalf("a settled message is still held: %+v", kept)
	}
}

// A SENTENCE HANDED BACK GOES TO THE RECIPIENT IT WAS TYPED AT, and never over a
// draft the person has started since.
func TestARecoveredSendNeverOverwritesNewerInput(t *testing.T) {
	a, _ := keepLab(t)
	failed := outboxSnapshot{scope: "abc", seq: 1, line: "the words that did not go"}

	// A page the person has typed into since: the sentence waits beside it.
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "something else")
	a.closeRoom()
	if a.recoverDraft(taskRecipient(7), failed) {
		t.Fatal("the recovered sentence was laid over a draft typed since")
	}
	kept := a.composerAt(taskRecipient(7))
	if got := kept.box.String(); got != "something else" {
		t.Fatalf("task 7's own line is now %q", got)
	}
	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "something else" {
		t.Fatalf("opening the page overwrote what was typed there: %q", got)
	}
	if _, held := a.takeRecovered(taskRecipient(7)); !held {
		t.Fatal("the recovered sentence was dropped rather than held")
	}
	a.closeRoom()

	// A page with nothing in it: the sentence goes straight back into its box,
	// and is there when the person walks in.
	if !a.recoverDraft(taskRecipient(8), failed) {
		t.Fatal("the recovered sentence did not reach an empty page")
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("a task's recovered sentence landed in the conversation: %q", got)
	}
	a.openRoom(8, "Port the loader")
	if got := a.input.String(); got != "the words that did not go" {
		t.Fatalf("the page opened holding %q", got)
	}
}

// AND A SENTENCE HELD FOR A PAGE IS OFFERED THE MOMENT THAT PAGE'S BOX IS EMPTY,
// which is the one moment it cannot overwrite anything.
func TestAHeldSentenceIsOfferedOnTheWayIntoItsPage(t *testing.T) {
	a, _ := keepLab(t)
	a.openRoom(7, "Fix the nil-map crash")
	typeInto(t, a, "mine")
	a.closeRoom()
	a.recoverDraft(taskRecipient(7), outboxSnapshot{scope: "abc", seq: 2, line: "kept for this page"})

	// Emptying that page's box is what makes room for it.
	a.atComposer(taskRecipient(7), func(state *composerState) { state.box.reset() })
	a.openRoom(7, "Fix the nil-map crash")
	if got := a.input.String(); got != "kept for this page" {
		t.Fatalf("the page opened holding %q", got)
	}
	if kept := a.composerAt(taskRecipient(7)).sends; len(kept) != 0 {
		t.Fatalf("the sentence was offered and also still held: %+v", kept)
	}
}
