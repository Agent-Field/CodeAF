package tui3

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

func TestHomeTakeKeepsEachChatsDraft(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, saved := range []bool{false, true} {
			name := "separate sessions"
			if shared {
				name = "shared session"
			}
			if saved {
				name += "/saved draft"
			} else {
				name += "/empty draft"
			}
			t.Run(name, func(t *testing.T) {
				lab := newHomeLab(t)
				work := lab.workspace("project")
				otherFile := lab.session("project", "0000000000000002", "Other chat", work, time.Now())
				arrivedFile := lab.session("project", heldCell, "Arriving chat", work, time.Now())
				drafts := t.TempDir()
				orphan := lab.app(otherFile)
				orphan.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(drafts, work) })
				orphan.input.setText("words for the other chat from an earlier window")
				if err := orphan.draftSaveOf(orphan.mainDraftText()).commit(); err != nil {
					t.Fatal(err)
				}
				orphanText, err := os.ReadFile(orphan.draftFile)
				if err != nil {
					t.Fatal(err)
				}
				orphanRecord, err := os.ReadFile(draftKeepPath(orphan.draftFile))
				if err != nil {
					t.Fatal(err)
				}
				want := ""
				if saved {
					previous := lab.app(arrivedFile)
					previous.draftFile = asWindow(t, deadPid(t), func() string { return DraftFile(drafts, work) })
					want = "words saved for the arriving chat"
					previous.input.setText(want)
					if err := previous.draftSaveOf(previous.mainDraftText()).commit(); err != nil {
						t.Fatal(err)
					}
				}
				a := lab.app(otherFile)
				a.width, a.height = 200, 70
				a.shared = shared
				a.draftFile = DraftFile(drafts, work)
				a.input.setText("  current words for the other chat  ")
				a.input.cursor = 8
				otherComposer := a.mainComposer()
				otherDraft := a.draftFile
				arrivedDraft := DraftFile(drafts, work)
				a.open = func(workspace, file string) (Conversation, error) {
					return Conversation{Agent: &fakeAgent{model: "m"}, Workspace: workspace, SessionFile: file, DraftFile: arrivedDraft}, nil
				}
				src := &continuationListing{row: leftOffRow(heldCell, "other device", chatlist.Running, time.Minute)}
				taker := &fakeTaker{taken: Taken{Transcript: arrivedFile}}
				taker.on = func(string) { src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here }) }
				a.machines, a.taker = src, taker
				drain(t, a, a.openHome())
				standOn(t, a, heldCell)
				confirm(t, a)
				if a.at(pageHome) || a.file != arrivedFile {
					t.Fatal("continuation did not open the selected chat")
				}
				if got := a.input.String(); got != want {
					t.Errorf("arrived composer = %q, want %q", got, want)
				}
				spend(t, a, a.saveDraft(a.draftFile))
				if got := readDraft(arrivedDraft); got != want {
					t.Errorf("arrived draft file = %q, want %q", got, want)
				}
				if got := readDraft(otherDraft); got != string(otherComposer.box.value) {
					t.Errorf("other chat's draft changed to %q", got)
				}
				for path, want := range map[string][]byte{orphan.draftFile: orphanText, draftKeepPath(orphan.draftFile): orphanRecord} {
					got, err := os.ReadFile(path)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Errorf("another chat's saved draft changed: %v", err)
					}
				}
				if !shared {
					if kept := a.behind[a.convKey(otherFile)]; kept == nil || kept.side.draft != string(otherComposer.box.value) {
						t.Error("the other chat did not keep its composer")
					}
				}
			})
		}
	}
}

func TestFailedHomeTakeKeepsTheOtherChatsComposer(t *testing.T) {
	src := &continuationListing{row: leftOffRow("another-chat", "other device", chatlist.Running, time.Minute)}
	a, _ := heldLab(t, src, &fakeTaker{err: errors.New("move could not finish")})
	a.closeHome()
	a.input.setText("  words for the current chat  ")
	a.input.cursor = 5
	want := a.mainComposer()
	drain(t, a, a.openHome())
	standOn(t, a, src.row.Cell)
	confirm(t, a)
	if got := a.mainComposer(); !reflect.DeepEqual(got, want) {
		t.Errorf("failed continuation changed the other chat's composer: %+v", got)
	}
}
