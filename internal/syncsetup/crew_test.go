package syncsetup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestCrewOfChatNamesTheChatByPathCopyOrMovedCell(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chat")
	transcript := session.Place{Dir: root}.Transcript()
	crew := []teams.Team{
		{ID: "c1", Name: "Todo crew", Members: []teams.Member{{Key: transcript, File: transcript}}, Manager: transcript},
		{ID: "f1", Name: "Item 7", FactoryManaged: true, Members: []teams.Member{{Key: teams.RemoteKey("moved")}}},
	}
	if got := CrewOfChat(crew, root, "id"); got != (chatlist.Crew{ID: "c1", Name: "Todo crew", Manager: true}) {
		t.Fatalf("by transcript = %+v", got)
	}
	// An older chat running from its finished moving copy is the same member.
	if got := CrewOfChat(crew, cell.CopyRoot(root), "id"); got.ID != "c1" {
		t.Fatalf("by moving copy = %+v", got)
	}
	// A member that moved to another computer is named by its cell.
	if got := CrewOfChat(crew, "", "moved"); got != (chatlist.Crew{ID: "f1", Name: "Item 7", Kind: chatlist.CrewKindFactory}) {
		t.Fatalf("by moved cell = %+v", got)
	}
	if got := CrewOfChat(crew, filepath.Join(t.TempDir(), "other"), "other"); got != (chatlist.Crew{}) {
		t.Fatalf("a chat in no crew = %+v", got)
	}
}

// TestCrewMadeOnOneComputerAppearsOnTheOther is the round trip through a real
// relay and the real engine: a crew made on home A is read on home B, from the
// chat's directory record alone, with its id, name and manager; and when the
// chat leaves the crew, B stops showing it in one at A's next saved turn.
func TestCrewMadeOnOneComputerAppearsOnTheOther(t *testing.T) {
	r := newDriveRig(t)
	transcript := transcriptOf(r.cell)
	if err := teams.Update(r.a.profile(), func(f *teams.File) error {
		f.Teams = []teams.Team{{ID: "da48d479ea82", Name: "Todo crew", Members: []teams.Member{{Key: transcript, File: transcript, Word: "todo"}}, Manager: transcript}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var notices noticeLog
	c := r.startChat(&notices)
	c.mustSay()
	waitFor(t, "the turn to be durable", func() bool { return r.directoryHead(r.cell.ID) == r.head() })
	crewOnB := func() chatlist.Crew {
		rows, _, _, err := r.b.RowsMailAt(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Cell == r.cell.ID {
				return row.Crew
			}
		}
		t.Fatalf("home B does not list the chat: %+v", rows)
		return chatlist.Crew{}
	}
	if got := crewOnB(); got != (chatlist.Crew{V: 1, ID: "da48d479ea82", Name: "Todo crew", Manager: true}) {
		t.Fatalf("home B reads the crew as %+v", got)
	}

	if err := teams.Update(r.a.profile(), func(f *teams.File) error { f.Teams = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	c.mustSay()
	waitFor(t, "the second turn to be durable", func() bool { return r.directoryHead(r.cell.ID) == r.head() })
	if got := crewOnB(); got != (chatlist.Crew{}) {
		t.Fatalf("a chat that left its crew still reads as in %+v", got)
	}
}
