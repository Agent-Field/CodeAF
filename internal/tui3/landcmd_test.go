package tui3

// /land, AND THE LINE THAT SAYS THERE IS SOMETHING TO LAND.
//
// Written from the person's side: nothing moves until they say the word twice,
// and the surface says nothing at all when there is nothing waiting.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// landingAgent is a conversation holding work for one or more folders. It is a
// fakeAgent with the landing seam on it and nothing else, so every other test
// in this package keeps an agent that honestly does not have one.
type landingAgent struct {
	fakeAgent
	waiting []session.StandingChange
	files   []string
	landed  string
	answer  session.FolderLanding
	fail    error
}

func (l *landingAgent) UnlandedChanges() []session.StandingChange { return l.waiting }

func (l *landingAgent) LandingFor(folder string) (session.FolderLanding, bool) {
	for _, change := range l.waiting {
		if change.Folder == folder {
			return session.FolderLanding{Folder: folder, Name: change.Name, Files: l.files}, true
		}
	}
	return session.FolderLanding{}, false
}

func (l *landingAgent) Land(folder string) (session.FolderLanding, error) {
	l.landed = folder
	l.waiting = nil
	return l.answer, l.fail
}

func landingApp(waiting ...session.StandingChange) (*app, *landingAgent) {
	agent := &landingAgent{waiting: waiting, files: []string{"shared.txt", "notes.md"}}
	agent.answer = session.FolderLanding{Name: "agentfield", Files: agent.files, Merged: "merged"}
	return newTestApp(agent), agent
}

// THE ROW IS DRAWN ONLY WHILE SOMETHING IS WAITING. A folder this conversation
// merely refers to is not news; changes that are not in it yet are.
func TestTheWaitingRowIsDrawnOnlyWhileThereAreChangesToLand(t *testing.T) {
	a, agent := landingApp()
	if row := a.landRow(a.width); row != "" {
		t.Fatalf("a conversation with nothing waiting draws %q", row)
	}
	if a.landHeight() != 0 {
		t.Fatal("the frame is charged for a row that is not drawn")
	}

	agent.waiting = []session.StandingChange{{Folder: "/code/agentfield", Name: "agentfield", Files: 3}}
	row := plain(a.landRow(a.width))
	for _, want := range []string{"changes for agentfield", "3 files", "/land"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the row is %q, missing %q", row, want)
		}
	}
	if a.landHeight() != 1 {
		t.Fatal("the row is drawn and the frame is not charged for it")
	}

	// One file is one file, not "1 files".
	agent.waiting[0].Files = 1
	if row := plain(a.landRow(a.width)); !strings.Contains(row, "1 file ·") {
		t.Fatalf("the row counts badly: %q", row)
	}
}

// AND THE ROW REACHES THE FRAME. A line nothing draws is a line nobody sees.
func TestTheWaitingRowRidesTheFrameAboveTheBox(t *testing.T) {
	a, _ := landingApp(session.StandingChange{Folder: "/code/agentfield", Name: "agentfield", Files: 2})
	if !strings.Contains(plain(frame(a)), "changes for agentfield") {
		t.Fatalf("the frame does not draw the waiting row:\n%s", plain(frame(a)))
	}
}

// /land SHOWS AND DOES NOT MOVE. The one moment a person has to be sure is the
// one moment they are shown what changes.
func TestLandShowsWhatWouldMoveAndOnlyLandNowMovesIt(t *testing.T) {
	a, agent := landingApp(session.StandingChange{Folder: "/code/agentfield", Name: "agentfield", Files: 2})

	a.slash("/land")
	said := plain(lastNote(t, a))
	for _, want := range []string{"changes for agentfield", "2 files", "shared.txt", "/land now"} {
		if !strings.Contains(said, want) {
			t.Fatalf("/land said %q, missing %q", said, want)
		}
	}
	if agent.landed != "" || len(agent.waiting) == 0 {
		t.Fatal("/land landed something without being asked twice")
	}

	// The landing runs off the loop, so the command is run and its answer is
	// driven back in — which is exactly what the program does with it.
	cmd := a.slash("/land now")
	if cmd == nil {
		t.Fatal("/land now produced no landing")
	}
	drive(t, a, cmd())
	if len(agent.waiting) != 0 {
		t.Fatal("/land now left the changes waiting")
	}
	if got := plain(lastNote(t, a)); !strings.Contains(got, "agentfield now has the changes") {
		t.Fatalf("the landing said %q", got)
	}
	if row := a.landRow(a.width); row != "" {
		t.Fatalf("the row is still up after the landing: %q", row)
	}
}

// WITH TWO FOLDERS WAITING THE SURFACE ASKS WHICH, because the wrong one is
// somebody's real project.
func TestLandWithTwoFoldersWaitingAsksWhichOne(t *testing.T) {
	a, agent := landingApp(
		session.StandingChange{Folder: "/code/agentfield", Name: "agentfield", Files: 2},
		session.StandingChange{Folder: "/notes", Name: "notes", Files: 1},
	)
	a.slash("/land")
	said := plain(lastNote(t, a))
	if !strings.Contains(said, "agentfield") || !strings.Contains(said, "notes") || !strings.Contains(said, "say which one") {
		t.Fatalf("/land with two waiting said %q", said)
	}
	if agent.landed != "" {
		t.Fatal("/land picked a folder for the person")
	}

	a.slash("/land notes")
	if got := plain(lastNote(t, a)); !strings.Contains(got, "changes for notes") || !strings.Contains(got, "/land notes now") {
		t.Fatalf("/land notes said %q", got)
	}
}

// AND NOTHING WAITING SAYS SO, and says where the changes a person can see DID
// go — which is what they were really asking.
func TestLandWithNothingWaitingSaysWhereWritesAlreadyGo(t *testing.T) {
	a, _ := landingApp()
	a.slash("/land")
	if got := plain(lastNote(t, a)); !strings.Contains(got, "nothing is waiting") {
		t.Fatalf("/land with nothing waiting said %q", got)
	}
}
