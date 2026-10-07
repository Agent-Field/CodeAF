package tui3

import (
	"fmt"
	"strings"
	"testing"
)

// stripHitKind is the first piece of kind on the last laid-out strip.
func stripHitKind(t *testing.T, a *app, kind tabKind) tabHit {
	t.Helper()
	for _, hit := range a.chatTabHits {
		if hit.kind == kind {
			return hit
		}
	}
	t.Fatalf("no strip piece of kind %d on %q\n%+v", kind, plain(a.tabsRow(a.width)), a.chatTabHits)
	return tabHit{}
}

// THE MANAGER'S PLACE IS THE FIRST TAB. A team shown with no manager starts
// its run of tabs with a quiet `+ Manager`, which has no conversation behind it
// and no close cells. Pressed, it starts a conversation in the team's folder,
// which joins the team and becomes its manager, on disk as in the window, and
// the place then reads `◆ harbor` and is that conversation.
func TestTeamManagerSlotStartsAManager(t *testing.T) {
	a, harbor := managerApp(t)
	n := 0
	var where string
	a.start = func(workspace string) (Conversation, error) {
		n++
		where = workspace
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: fmt.Sprintf("/tmp/lab/boss-%d.jsonl", n), Workspace: workspace}, nil
	}
	a.touch()
	row := plain(a.tabsRow(a.width))
	slot := stripHitKind(t, a, tabManager)
	if got := strings.TrimSpace(plainCells(row, slot.span.from, slot.span.to)); got != teamManagerSlotWord {
		t.Fatalf("the manager's place reads %q on %q", got, row)
	}
	for _, hit := range a.chatTabHits {
		if hit.kind == tabOther || hit.kind == tabHere {
			if hit.span.from < slot.span.from {
				t.Fatalf("a member is drawn before the manager's place: %q", row)
			}
		}
		if hit.kind == tabClose && hit.tab.slot {
			t.Fatal("the manager's empty place has close cells")
		}
	}

	cmd, took := a.tabPress(slot.span.from+1, tabStripRow)
	if !took {
		t.Fatal("the strip did not take the press")
	}
	// The conversation is opened on the door line, off the loop.
	spend(t, a, cmd)
	if n != 1 {
		t.Fatalf("the press started %d conversations", n)
	}
	if where != "/tmp/lab" {
		t.Fatalf("the manager was started in %q, not the team's folder", where)
	}
	boss := a.frontTabKey()
	got := mustTeam(t, a, harbor)
	if got.Manager != boss || !got.Holds(boss) {
		t.Fatalf("the new conversation is not the manager: %q, %+v", got.Manager, got.Members)
	}
	teamsFlush(t, a)
	disk, _ := loadTeams(a.profileDir, nil)
	if len(disk) == 0 || disk[0].Manager != boss {
		t.Fatalf("the manager did not reach the disk: %+v", disk)
	}

	// The place is now the manager's own tab, named for the team.
	a.touch()
	row = plain(a.tabsRow(a.width))
	for _, hit := range a.chatTabHits {
		if hit.kind == tabManager {
			t.Fatalf("the empty place is still drawn beside a manager: %q", row)
		}
	}
	first := tabHit{span: hudSpan{from: 1 << 30}}
	for _, hit := range a.chatTabHits {
		if (hit.kind == tabOther || hit.kind == tabHere) && hit.span.from < first.span.from {
			first = hit
		}
	}
	if first.tab.key != boss || !strings.Contains(row, teamManagerGlyph+" "+teamManagerWord) {
		t.Fatalf("the first tab is %+v on %q, want the manager as ◆ Manager", first.tab, row)
	}
}

// managerApp is [menuApp] with its teams on a profile of the test's own.
func managerApp(t *testing.T) (*app, string) {
	t.Helper()
	a, harbor, _ := menuApp(t)
	a.profileDir = t.TempDir()
	if err := saveTeams(a.profileDir, a.wall.teams); err != nil {
		t.Fatal(err)
	}
	return a, harbor
}

// mustTeam is team id as the window holds it.
func mustTeam(t *testing.T, a *app, id string) team {
	t.Helper()
	got, ok := a.teamByID(id)
	if !ok {
		t.Fatalf("no team %s", id)
	}
	return got
}

// plainCells is the plain cells from to to of a plain row.
func plainCells(row string, from, to int) string {
	r := []rune(row)
	if from < 0 || to > len(r) || from >= to {
		return ""
	}
	return string(r[from:to])
}

// MAKE MANAGER AND REMOVE MANAGER ARE ONE ROW OF THE SWITCHER. On a member of
// the team shown it makes that conversation the manager; on the manager it
// reads Remove manager and leaves it an ordinary member. The menu stays up so
// the word is seen to flip.
func TestOverlayPickerCannotChangeManager(t *testing.T) {
	a, harbor := managerApp(t)
	before := mustTeam(t, a, harbor).Manager
	a.openTeamMenu()
	frame, _ := menuFrame(t, a)
	if strings.Contains(frame, "Make this harbor's manager") || strings.Contains(frame, "ordinary member") {
		t.Fatal(frame)
	}
	if mustTeam(t, a, harbor).Manager != before {
		t.Fatal("overlay picker changed manager")
	}
}

// ON THE WALL, A TILE'S TEAMS POPOVER CARRIES THE SAME ROW for its one
// conversation in the team shown, and the manager's tile is pinned first and
// marked.
func TestConversationGridDoesNotPinTeamManagers(t *testing.T) {
	a, harbor := managerApp(t)
	tabs := a.tabList()
	if err := a.teamToggleManager(harbor, tabs[len(tabs)-1]); err != nil {
		t.Fatal(err)
	}
	a.teamViewSet(harbor)
	_ = a.openWall()
	tiles := a.wallShown(a.now())
	for i, tile := range tiles {
		if tile.manager || tile.tab.key != tabs[i].key {
			t.Fatal("grid reordered conversations around team manager")
		}
	}
}
