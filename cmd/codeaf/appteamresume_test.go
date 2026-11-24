package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// A CREW MANAGER THAT MOVED TO ANOTHER WINDOW, WHICH THEN CLOSED, IS OPENED
// AGAIN IN THE APP by the team door, so its members' replies wake it where the
// crew is drawn; its row stops saying it moved. A transcript that is not one of
// the app's agents is left to the folder's host.
func TestAppTeamDoorBringsAMovedManagerBack(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	b, rec, _, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, "off")
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := b.Send(agent.ID, "manage the crew", "first")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	transcript := appTranscriptOf(b, agent.ID)
	if err := session.AskTakeover(v3PlaceOf(transcript, workspace).Dir); err != nil {
		t.Fatal(err)
	}
	entryWith(rec, t, func(e appwire.Entry) bool {
		return e.NoticeKind == appwire.NoticeMoved && e.Text == session.TakeoverWord
	})
	appSyncWait(t, "the takeover to let the journal go", func() bool { return !session.InUse(transcript) })

	if mine, err := b.reopenForTeam(filepath.Join(t.TempDir(), "elsewhere", "transcript.jsonl")); mine || err != nil {
		t.Fatalf("a transcript the app does not hold was taken as its own: %v %v", mine, err)
	}
	mine, err := b.reopenForTeam(transcript)
	if !mine || err != nil {
		t.Fatalf("the moved manager was not reopened: mine=%v err=%v", mine, err)
	}
	if !session.InUse(transcript) {
		t.Fatal("the reopened manager does not hold its journal")
	}
	view, err := b.OpenAgent(agent.ID)
	if err != nil {
		t.Fatalf("the reopened manager still refuses: %v", err)
	}
	if view.Location != nil && !view.Location.Here {
		t.Fatalf("the row still says the manager moved: %+v", view.Location)
	}
	if _, err := b.Send(agent.ID, "carry on", "second"); err != nil {
		t.Fatalf("the reopened manager refuses work: %v", err)
	}
}
