package tui3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

func catalogLab(t *testing.T, count int) (*app, []session.SessionRow) {
	t.Helper()
	a, _, _ := tabApp(t)
	var rows []session.SessionRow
	root := t.TempDir()
	for i := 0; i < count; i++ {
		dir := filepath.Join(root, fmt.Sprintf("conversation-%02d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "transcript.jsonl")
		text := fmt.Sprintf("Saved reply %02d", i)
		if err := os.WriteFile(file, []byte(fmt.Sprintf("{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", text)), 0600); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, session.SessionRow{ID: filepath.Base(dir), Transcript: file, Title: fmt.Sprintf("Saved conversation Item%02d", i), ProjectDir: root, At: time.Date(2026, 10, 3, 12, i, 0, 0, time.UTC)})
	}
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{Sessions: rows}}}, true
	}
	return a, rows
}

// The library is larger than the switcher's visible card and does not depend
// on tabs. Both pickers and the grid discover the same saved conversations.
func TestConversationCatalogGridIncludesClosedAndArchivedWithoutOpening(t *testing.T) {
	a, rows := catalogLab(t, 45)
	rows[0].Archived = true
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{Sessions: rows}}}, true
	}
	opened := 0
	a.open = func(string, string) (Conversation, error) {
		opened++
		return Conversation{}, fmt.Errorf("unexpected open")
	}
	_ = a.openWall()
	a.wall.catalogBusy = false
	a.wall.catalogAt = time.Time{}
	drive(t, a, runCmd(a.wallCatalogRead())...)
	tiles := a.wallShown(a.now())
	if len(tiles) != 48 || opened != 0 {
		t.Fatalf("catalog size=%d opened=%d", len(tiles), opened)
	}
	found := map[string]bool{}
	for _, tile := range tiles {
		found[tile.tab.file] = true
	}
	for _, row := range rows {
		if !found[row.Transcript] {
			t.Fatalf("saved conversation omitted: %s", row.Title)
		}
	}
	a.closeWall()
	drive(t, a, runCmd(a.teamCreateOpen(""))...)
	if len(a.tcreate.rows) != 48 || a.wall.on {
		t.Fatal("creation picker differs from the full catalog or opened grid")
	}
}

func TestConversationGridDismissedTabRemainsAndDeletedConversationDoesNot(t *testing.T) {
	a, rows := catalogLab(t, 2)
	_ = a.openWall()
	a.wall.catalog = a.conversationCatalog(session.World{Projects: []session.Project{{Sessions: rows}}})
	before := a.wallShown(a.now())
	dismissed := before[0].tab.key
	_ = a.wallDismissAt(before, 0)
	if !a.tabShut[dismissed] {
		t.Fatal("tab was not dismissed")
	}
	exists := false
	for _, tile := range a.wallShown(a.now()) {
		if tile.tab.key == dismissed {
			exists = true
			if !tile.noTab {
				t.Fatal("dismissed tab still offers Close")
			}
		}
	}
	if !exists {
		t.Fatal("dismissing a tab removed the saved conversation from the grid")
	}
	if err := os.Remove(rows[0].Transcript); err != nil {
		t.Fatal(err)
	}
	a.wall.savedAt = nil
	a.wall.focus = 0
	a.wall.cols = 6
	drive(t, a, runCmd(a.wallSavedReadCmd())...)
	for _, tile := range a.wallShown(a.now()) {
		if tile.tab.file == rows[0].Transcript {
			t.Fatal("deleted transcript stayed in grid")
		}
	}
}

// A grid selection is an identity set, not the currently filtered slice.
func TestConversationGridCreateKeepsHiddenSelectionsAndDoesNotResumeMembers(t *testing.T) {
	a, rows := catalogLab(t, 3)
	_ = a.openWall()
	a.wall.catalog = a.conversationCatalog(session.World{Projects: []session.Project{{Sessions: rows}}})
	_ = a.wallFrame(a.width, a.height)
	create := wallHitFor(t, a, wallHitAction, int(wallActNewTeam))
	_ = wallClick(t, a, create)
	if !a.wall.selecting || a.wall.naming || len(a.wall.marked) != 0 {
		t.Fatal("New team did not start an empty selection")
	}
	for _, r := range rows[:2] {
		a.wall.filter = strings.Fields(r.Title)[2]
		a.wall.focus = 0
		_ = a.wallFrame(a.width, a.height)
		_ = wallClick(t, a, wallHitFor(t, a, wallHitTile, 0))
	}
	if len(a.wall.marked) != 2 {
		t.Fatalf("card clicks did not select independently of filter: marked=%v shown=%v selecting=%v", a.wall.marked, a.wallShown(a.now()), a.wall.selecting)
	}
	_ = a.wallStartNaming(a.wallShown(a.now()))
	a.wall.name = "A grid-created team"
	before := a.frontTabKey()
	drive(t, a, runCmd(a.wallMakeTeam(a.wallShown(a.now())))...)
	made, ok := a.teamsSelected()
	if !ok || made.Name != "A grid-created team" || len(made.Members) != 2 || !made.Holds(a.convKey(rows[0].Transcript)) || !made.Holds(a.convKey(rows[1].Transcript)) {
		t.Fatalf("wrong team: %+v", made)
	}
	if !a.at(pageTeams) || a.wall.on || a.frontTabKey() != before || a.behind[a.convKey(rows[0].Transcript)] != nil {
		t.Fatal("creation opened a member or failed to land in Teams")
	}
	drive(t, a, runCmd(a.teamsWrite())...)
	stored, err := teamstore.Load(a.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := stored.Team(made.ID); !ok || len(actual.Members) != 2 {
		t.Fatal("selected saved chats were not persisted")
	}
}

func TestTeamsCreateEmptyAndMultiSelectWithoutOpeningGrid(t *testing.T) {
	for _, members := range []int{0, 2} {
		t.Run(fmt.Sprint(members), func(t *testing.T) {
			a, rows := catalogLab(t, 3)
			drive(t, a, runCmd(a.showPage(pageTeams))...)
			before := a.frontTabKey()
			a.input.setText("Preserve my draft")
			drive(t, a, runCmd(a.teamMenuNewTeam())...)
			frame, _, _ := a.frame()
			if a.wall.on || !a.at(pageTeams) || !strings.Contains(ansi.Strip(frame), "Members · optional") {
				t.Fatal("creation commandeered Chats grid")
			}
			for _, r := range rows[:members] {
				a.tcreate.filter.setText(r.Title)
				a.tcreate.cursor = 0
				a.touch()
				_, _, _ = a.frame()
				var hit wallHit
				for _, h := range a.tcreate.hits {
					if h.kind == wallHitSelect {
						hit = h
						break
					}
				}
				drive(t, a, nestPress(hit.x0, hit.y0))
				if len(a.tcreate.selected) == 0 {
					t.Fatalf("click did not select: hit=%+v shown=%+v filter=%q hits=%+v", hit, a.tcreate.shown, a.tcreate.filter.String(), a.tcreate.hits)
				}
			}
			a.tcreate.name.setText("Made in Teams")
			drive(t, a, key("enter"))
			made, ok := a.teamsSelected()
			if !ok || made.Name != "Made in Teams" || len(made.Members) != members || made.Manager != "" {
				t.Fatalf("wrong new team: %+v", made)
			}
			if a.frontTabKey() != before || a.input.String() != "Preserve my draft" || a.tcreate.on {
				t.Fatal("creation changed conversation or draft")
			}
			drive(t, a, runCmd(a.teamsWrite())...)
			stored, err := teamstore.Load(a.profileDir)
			if err != nil {
				t.Fatal(err)
			}
			if actual, ok := stored.Team(made.ID); !ok || len(actual.Members) != members {
				t.Fatal("creation was not saved")
			}
		})
	}
}

func TestTeamsCreateCancelDropsLateCatalogAndKeepsSelectionAndDraft(t *testing.T) {
	a, rows := catalogLab(t, 2)
	drive(t, a, runCmd(a.showPage(pageTeams))...)
	before := a.frontTabKey()
	a.input.setText("Existing draft")
	cmd := a.teamCreateOpen("")
	a.teamCreateShut()
	drive(t, a, runCmd(cmd)...)
	if a.tcreate.on || len(a.tcreate.rows) != 0 || !a.at(pageTeams) || a.frontTabKey() != before || a.input.String() != "Existing draft" {
		t.Fatal("late catalog resurrected cancelled dialog")
	}
	old := a.wall.catalogGen
	_ = a.openWall()
	a.closeWall()
	_ = a.openWall()
	a.wallTakeSavedRead(wallSavedReadMsg{generation: old, missing: []string{a.frontTabKey()}, reads: []wallReadMsg{{key: a.convKey(rows[0].Transcript), at: a.now()}}})
	if a.wall.tails[a.convKey(rows[0].Transcript)] != nil {
		t.Fatal("stale preview entered reopened grid")
	}
}

func TestTeamCreatePickerScrollAndHitsMatchRows(t *testing.T) {
	a, _ := catalogLab(t, 45)
	drive(t, a, runCmd(a.showPage(pageTeams))...)
	drive(t, a, runCmd(a.teamCreateOpen(""))...)
	for _, sz := range [][2]int{{40, 14}, {80, 24}, {160, 45}} {
		a.width, a.height = sz[0], sz[1]
		a.tcreate.filter.reset()
		a.tcreate.cursor = 44
		a.tcreate.field = 2
		a.tcreate.message = "Give the team a name"
		a.touch()
		frame, _, _ := a.frame()
		if a.tcreate.rect.y1 > a.height {
			t.Fatalf("dialog clipped at %v", sz)
		}
		screen := strings.Split(frame, "\n")
		found := false
		for _, h := range a.tcreate.hits {
			if h.x0 < 0 || h.x1 > a.width || h.y0 < 0 || h.y1 > a.height {
				t.Fatalf("hit outside frame: %+v", h)
			}
			if h.kind == wallHitSelect && h.arg == 44 {
				found = true
				if got := ansi.Strip(ansi.Cut(screen[h.y0], h.x0, h.x1)); !strings.Contains(got, "Saved") {
					t.Fatalf("selection target names another row: %q", got)
				}
			}
		}
		if !found {
			t.Fatal("last candidate was inaccessible")
		}
	}
}

func TestConversationSavedPreviewIsBoundedAndHostNeverReadsLocalPaths(t *testing.T) {
	a, rows := catalogLab(t, 1)
	data := strings.Repeat("{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"old\"}\n", 2000) + "{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Newest saved reply\"}\n"
	if err := os.WriteFile(rows[0].Transcript, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	entries, _, err := wallReadSavedTail(rows[0].Transcript)
	if err != nil || len(entries) >= 2000 || entries[len(entries)-1].Text != "Newest saved reply" {
		t.Fatalf("unbounded or incorrect preview: %d, %v", len(entries), err)
	}
	_ = a.openWall()
	a.wall.catalog = a.conversationCatalog(session.World{Projects: []session.Project{{Sessions: rows}}})
	a.host = "elsewhere"
	if cmd := a.wallSavedReadCmd(); cmd != nil {
		t.Fatal("hosted grid attempted local reads")
	}
}

func TestTeamCreationRejectsExistingNameAndClosedParent(t *testing.T) {
	a, harbor, _ := menuApp(t)
	drive(t, a, runCmd(a.showPage(pageTeams))...)
	drive(t, a, runCmd(a.teamCreateOpen(""))...)
	_, _, _ = a.frame()
	before := mustTeam(t, a, harbor)
	a.tcreate.name.setText(before.Name)
	if cmd := a.teamCreateSave(); cmd != nil || !a.tcreate.on || mustTeam(t, a, harbor).Manager != before.Manager {
		t.Fatal("duplicate name changed existing team")
	}
	a.teamCreateShut()
	if _, err := a.teamCreateIn("Duplicate", nil, teamHueSpec{}, harbor); err != nil {
		t.Fatal(err)
	}
	if _, err := a.teamCreateIn("Duplicate", nil, teamHueSpec{}, ""); err == nil {
		t.Fatal("shared creation edit allowed duplicate name")
	}
	if err := a.teamEdit(func(f *teamstore.File) error { return f.Disband(harbor, a.now(), "") }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.teamCreateIn("Late child", nil, teamHueSpec{}, harbor); err == nil {
		t.Fatal("creation allowed a disbanded parent")
	}
}

// Canonicalizing saved paths belongs to the catalog command. A redraw must
// never walk the saved library through the filesystem's symlink resolver.
func TestConversationCatalogPaintingDoesNotResolveSavedPaths(t *testing.T) {
	a, rows := catalogLab(t, 45)
	_ = a.openWall()
	a.wall.catalog = a.conversationCatalog(session.World{Projects: []session.Project{{Sessions: rows}}})
	saved := map[string]bool{}
	for _, row := range rows {
		saved[row.Transcript] = true
	}
	prior := resolveTranscript
	defer func() { resolveTranscript = prior }()
	walked := 0
	resolveTranscript = func(file string) (string, error) {
		if saved[file] {
			walked++
		}
		return file, nil
	}
	for range 3 {
		a.touch()
		a.frame()
	}
	if walked != 0 {
		t.Fatalf("painting resolved %d saved paths", walked)
	}
}

func TestConversationGridSharedRememberedTabStillOffersClose(t *testing.T) {
	a, _, _ := sharedSurface(t)
	a.width, a.height = 160, 40
	remembered := rememberUnheldTab(a, "/srv/app/b.jsonl", "/srv/app", "Porting")
	_ = a.openWall()
	found := false
	for _, tile := range a.wallShown(a.now()) {
		if tile.tab.key == remembered.key {
			found = true
			if tile.live || tile.noTab {
				t.Fatal("remembered shared tab lost Close or pretended to be live")
			}
		}
	}
	if !found {
		t.Fatal("remembered shared tab omitted")
	}
}

func TestTeamsCreateKeyboardSelectionAndSaveFailureReceipt(t *testing.T) {
	a, _ := catalogLab(t, 2)
	drive(t, a, runCmd(a.showPage(pageTeams))...)
	a.teamsDisk.door = refusingSeam(a, teamstore.ErrBusy)
	drive(t, a, runCmd(a.teamCreateOpen(""))...)
	drive(t, a, tea.PasteMsg{Content: "Keyboard team"}, key("tab"), key("tab"))
	a.frame()
	drive(t, a, key(" "))
	if len(a.tcreate.selected) != 1 {
		t.Fatal("space did not select a candidate")
	}
	drive(t, a, key("enter"))
	if !a.at(pageTeams) || a.tcreate.on {
		t.Fatal("keyboard creation failed to land on the Teams overview")
	}
	drive(t, a, runCmd(a.teamsWrite())...)
	if text := teamsFrameText(a); !strings.Contains(text, "Keyboard team was not saved") || strings.Contains(text, "Created Keyboard team") {
		t.Fatalf("creation save refusal was hidden:\n%s", text)
	}
}

func TestTeamCreationRechecksParentAtPersistence(t *testing.T) {
	for _, closed := range []bool{false, true} {
		a, harbor, _ := menuApp(t)
		parent := mustTeam(t, a, harbor)
		a.teamsDisk.queue, a.teamsDisk.queueSeq = nil, nil
		id, err := a.teamCreateIn("Late child", nil, teamHueSpec{}, harbor)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.teamsDisk.queue) != 1 {
			t.Fatal("creation did not enqueue exactly one edit")
		}
		if closed {
			parent.State = teamstore.TeamClosed
		} else {
			limit := 1
			parent.Settings.DepthLimit = &limit
		}
		current := &teamstore.File{Teams: []teamstore.Team{parent}}
		if err := a.teamsDisk.queue[0](current); err == nil {
			t.Fatalf("changed parent accepted child %s (closed=%v)", id, closed)
		}
		if _, exists := current.Team(id); exists {
			t.Fatal("refused child was added to the store")
		}
	}
}

func TestTeamsCreateAndGridPastePreserveConversationDraft(t *testing.T) {
	a, _ := catalogLab(t, 2)
	a.input.setText("Original draft")
	drive(t, a, runCmd(a.showPage(pageTeams))...)
	drive(t, a, runCmd(a.teamCreateOpen(""))...)
	drive(t, a, tea.PasteMsg{Content: "Pasted\r\nname"}, key("tab"), tea.PasteMsg{Content: "Ignored on colour"}, key("tab"), tea.PasteMsg{Content: "Saved conversation"})
	if a.tcreate.name.String() != "Pasted name" || a.tcreate.filter.String() != "Saved conversation" || a.input.String() != "Original draft" {
		t.Fatal("creation paste did not stay in its active field")
	}
	drive(t, a, key("esc"))
	_ = a.openWall()
	a.wall.filterOn = true
	drive(t, a, tea.PasteMsg{Content: "Saved"})
	if a.wall.filter != "Saved" || a.input.String() != "Original draft" {
		t.Fatal("grid filter paste reached the draft")
	}
	a.wall.selecting = true
	_ = a.wallStartNaming(a.wallShown(a.now()))
	drive(t, a, tea.PasteMsg{Content: "Grid pasted name"})
	if a.wall.name != "Grid pasted name" || a.wall.nameFresh || a.wall.nameAsking || a.input.String() != "Original draft" {
		t.Fatal("grid naming paste did not replace the suggestion safely")
	}
	a.closeWall()
	if a.input.String() != "Original draft" {
		t.Fatal("cancelling after pastes changed the original draft")
	}
}

func TestConversationGridSelectionHidesAnswerAndShowsNamingErrors(t *testing.T) {
	a, harbor, _ := menuApp(t)
	_ = a.openWall()
	a.wall.selecting = true
	v := wallView{selecting: true, tiles: []wallTile{{name: "Waiting", signal: tabNeedsPerson}}}
	if frame := strings.Join(wallPaintTile(a.pal, wallGlyphs{}, v, v.tiles[0], 0, true, 60, 25, 0), "\n"); strings.Contains(ansi.Strip(frame), "Answer") {
		t.Fatal("member selection still advertises Answer")
	}
	for _, h := range wallTileHits(a.pal, v, v.tiles[0], 0, true, 0, 0, 60, 25) {
		if h.kind == wallHitOpen {
			t.Fatal("member selection retained an Answer target")
		}
	}
	_ = a.wallStartNaming(a.wallShown(a.now()))
	a.wall.name = mustTeam(t, a, harbor).Name
	if cmd := a.wallMakeTeam(a.wallShown(a.now())); cmd != nil || !a.wall.naming {
		t.Fatal("duplicate name closed the naming dialog")
	}
	if frame := ansi.Strip(strings.Join(a.wallFrame(a.width, a.height), "\n")); !strings.Contains(frame, "A team already uses this name") {
		t.Fatal("grid naming validation was invisible")
	}
	drive(t, a, key("backspace"))
	if a.wall.nameError != "" {
		t.Fatal("editing did not clear the naming error")
	}
}
