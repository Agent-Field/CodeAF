package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Contracts G1.1, G1.3 and G1.4 spend the old draft when its chat returns elsewhere.
func TestContinueSendCannotRestoreItsWordsOrCreateAnotherChat(t *testing.T) {
	for _, retire := range []bool{false, true} {
		t.Run(map[bool]string{false: "moved folder", true: "retired session"}[retire], func(t *testing.T) {
			src := &continuationListing{row: leftOffRow(heldCell, "other computer", chatlist.Running, time.Minute)}
			taker := &fakeTaker{}
			a, old := heldLab(t, src, taker)
			a.closeHome()
			oldFile, oldWork := a.file, a.workspace
			drafts := t.TempDir()
			oldDraft := DraftFile(drafts, oldWork)
			a.draftFile = oldDraft
			words := "What is 19 times 21? Answer with just the number."
			a.input.setText(words)
			a.writeDraftsNow(a.leavingDraft())
			priorDraft := asWindow(t, deadPid(t), func() string { return DraftFile(drafts, oldWork) })
			prior := newTestApp(&fakeAgent{model: "m"})
			prior.file, prior.workspace, prior.draftFile = oldFile, oldWork, priorDraft
			prior.input.setText(words)
			prior.writeDraftsNow(prior.leavingDraft())
			arrivedWork := t.TempDir()
			arrivedFile := filepath.Join(arrivedWork, heldCell, ".cell", "transcript.jsonl")
			arrivedDraft := DraftFile(drafts, arrivedWork)
			arrived := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
			a.open = func(work, file string) (Conversation, error) {
				return Conversation{Agent: arrived, Workspace: arrivedWork, SessionFile: file, DraftFile: arrivedDraft}, nil
			}
			starts := 0
			start := a.start
			a.start = func(work string) (Conversation, error) { starts++; return start(work) }
			taker.taken = Taken{Transcript: arrivedFile, Fence: 2}
			taker.on = func(string) {
				if retire {
					drive(t, a, taskEventMsg{gen: a.taskGen, ev: session.Event{Kind: session.EventTakeover, Text: session.TakeoverWord}})
				}
				src.change(func(row *chatlist.Row) { row.Mine, row.Status, row.Fence = true, chatlist.Here, 2 })
			}
			drive(t, a, key("enter"))
			confirm(t, a)
			if a.file != arrivedFile || a.at(pageHome) {
				t.Fatalf("take did not open the arriving chat: %q", a.file)
			}
			if len(arrived.sent) == 0 {
				if got := readDraft(arrivedDraft); got != words {
					t.Errorf("arrived unsent draft=%q", got)
				}
				drive(t, a, key("enter"))
			}
			if !reflect.DeepEqual(arrived.sent, []string{words}) || len(old.sent) != 0 {
				t.Fatalf("sent=%v old=%v", arrived.sent, old.sent)
			}
			if starts != 0 || len(a.behind) != 0 {
				t.Errorf("take left another chat: starts=%d held=%d", starts, len(a.behind))
			}
			for _, file := range []string{oldDraft, arrivedDraft, priorDraft} {
				if got := readDraft(file); got != "" {
					t.Errorf("sent words remain in draft %s: %q", filepath.Base(file), got)
				}
				keep, _ := readDraftKeep(draftKeepPath(file))
				for _, slot := range keep.Slots {
					if slot.Task == 0 && slot.Run == "" && slot.Guest == "" && slot.Text != "" {
						t.Errorf("sent words remain in saved composer: %q", slot.Text)
					}
				}
			}
			reopened := newTestApp(&fakeAgent{model: "m"})
			reopened.file, reopened.workspace, reopened.draftFile = oldFile, oldWork, oldDraft
			reopened.restoreDraft()
			if got := reopened.mainDraftText(); got != "" {
				t.Errorf("later take-back restored sent words: %q", got)
			}
			later := &fakeAgent{model: "m"}
			a.open = func(work, file string) (Conversation, error) {
				return Conversation{Agent: later, Workspace: oldWork, SessionFile: file, DraftFile: oldDraft}, nil
			}
			taker.taken = Taken{Transcript: oldFile, Fence: 4}
			taker.on = func(string) {
				src.change(func(row *chatlist.Row) { row.Mine, row.Status, row.Fence = true, chatlist.Here, 4 })
			}
			src.change(func(row *chatlist.Row) { row.Mine, row.Status, row.Fence = false, chatlist.Running, 3 })
			spend(t, a, a.openHome())
			standOn(t, a, heldCell)
			confirm(t, a)
			if a.file != oldFile || a.mainDraftText() != "" {
				t.Fatalf("take-back opened %q with %q", a.file, a.mainDraftText())
			}
			typeText(t, a, "Double your last number. Just the number.")
			drive(t, a, key("enter"))
			if !reflect.DeepEqual(later.sent, []string{"Double your last number. Just the number."}) {
				t.Errorf("take-back repeated the old message: %v", later.sent)
			}
		})
	}
}

// Contracts G1.2–4 preserve one unsent composer through a refused take's retirement.
func TestFailedContinueKeepsOneDraftWithoutCreatingAChat(t *testing.T) {
	src := &continuationListing{row: leftOffRow(heldCell, "other computer", chatlist.Running, time.Minute)}
	taker := &fakeTaker{err: errors.New("folder unavailable")}
	a, old := heldLab(t, src, taker)
	a.closeHome()
	a.draftFile = DraftFile(t.TempDir(), a.workspace)
	a.input.setText("  keep these unsent words once  ")
	a.input.cursor = 6
	want := a.mainComposer()
	starts := 0
	start := a.start
	a.start = func(work string) (Conversation, error) { starts++; return start(work) }
	taker.on = func(string) {
		drive(t, a, taskEventMsg{gen: a.taskGen, ev: session.Event{Kind: session.EventTakeover, Text: session.TakeoverWord}})
	}
	drive(t, a, key("enter"))
	confirm(t, a)
	if got := a.mainComposer(); !reflect.DeepEqual(got, want) {
		t.Fatalf("failed take changed words: %+v", got)
	}
	a.writeDraftsNow(a.leavingDraft())
	if got := readDraft(a.draftFile); got != want.box.String() {
		t.Errorf("failed take's draft=%q", got)
	}
	if starts != 0 || len(a.behind) != 0 || len(old.sent) != 0 {
		t.Errorf("failed take made extra work: starts=%d held=%d sent=%v", starts, len(a.behind), old.sent)
	}
	if _, err := os.Stat(a.draftFile); err != nil {
		t.Fatal(err)
	}
}

// Contract G1.1 also spends matching older drafts when the chat is already held here.
func TestAlreadyHereContinuationSpendsTheEarlierWindowsDraft(t *testing.T) {
	src := &continuationListing{row: leftOffRow(heldCell, "other computer", chatlist.Running, time.Minute)}
	taker := &fakeTaker{}
	a, agent := heldLab(t, src, taker)
	a.closeHome()
	drafts := t.TempDir()
	a.draftFile = DraftFile(drafts, a.workspace)
	a.input.setText("send these words once")
	a.writeDraftsNow(a.leavingDraft())
	previous := newTestApp(&fakeAgent{model: "m"})
	previous.file, previous.workspace = a.file, a.workspace
	previous.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(drafts, a.workspace) })
	previous.input.setText(a.input.String())
	previous.writeDraftsNow(previous.leavingDraft())
	taker.taken = Taken{AlreadyHere: true, Transcript: a.file, Fence: 7}
	taker.on = func(string) {
		src.change(func(row *chatlist.Row) { row.Mine, row.Status, row.Fence = true, chatlist.Here, 7 })
	}
	drive(t, a, key("enter"))
	confirm(t, a)
	drive(t, a, key("enter"))
	if !reflect.DeepEqual(agent.sent, []string{"send these words once"}) || len(a.behind) != 0 {
		t.Fatalf("already-held continuation sent=%v held=%d", agent.sent, len(a.behind))
	}
	if got := readDraft(previous.draftFile); got != "" {
		t.Errorf("earlier window still holds sent words: %q", got)
	}
}
