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
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func TestTeamsPreviewReadsOnlyTheTailAndFollowsUpdates(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	old := strings.Repeat("x", teamsPreviewBytes*2) + "\n"
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(old+fmt.Sprintf("{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", text)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Latest update")
	first := teamsReadPreview(file, teamsPreview{})
	if first.text != "Latest update" {
		t.Fatalf("tail preview: %+v", first)
	}
	if same := teamsReadPreview(file, first); same != first {
		t.Fatal("unchanged journal changed the preview")
	}
	write("A newer and longer update")
	if got := teamsReadPreview(file, first).text; got != "A newer and longer update" {
		t.Fatal(got)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if gone := teamsReadPreview(file, first); !gone.missing || gone.text != "" {
		t.Fatalf("deleted file kept stale preview: %+v", gone)
	}
}

func TestTeamsOverviewReadInvalidatesAnExistingChatThread(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	a.width, a.height = 160, 40
	a.railAway = true
	handle, _ := trafficHandle(t, a, harbor, "openrouter")
	root := teamstore.Entry{ID: "000000000001", Kind: teamstore.KindNote, From: teamstore.FromManager, To: handle, Text: "status please"}
	a.teamsTakeInteractions(harbor, []teamstore.Entry{root})
	sendRow(a, `{"to":"@`+handle+`","text":"status please"}`, "Sent (#1).")
	showTeamDetails(a)
	_ = bodyText(a)
	a.teamsTakeInteractions(harbor, []teamstore.Entry{{ID: "000000000002", Kind: teamstore.KindNote, From: handle, To: teamstore.ToManager, Answers: root.ID, Text: "A fresh reply from the overview"}})
	if text := bodyText(a); !strings.Contains(text, "A fresh reply from the overview") {
		t.Fatal(text)
	}
}

func TestTeamsMembersPopupAliasClickNavigatesWithoutLosingDrag(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 120, 34
	a.teamCrewOpen(harbor)
	_ = teamsFrameText(a)
	rows := a.teamCrewRows()
	for _, hit := range a.tcrew.hits {
		if hit.kind != crewHitRow || hit.arg != 1 {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: hit.x0 + 2, Y: hit.y0, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: hit.x0 + 2, Y: hit.y0, Button: tea.MouseLeft})
		if a.at(pageTeams) || a.frontTabKey() != rows[1].key || a.wall.activeID != harbor {
			t.Fatal("alias click did not open the member")
		}
		return
	}
	t.Fatal("no member alias target")
}

func TestTeamsCurrentMemberDoorClosesTheTaskRoom(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.input.insert("conversation draft")
	a.room = &taskRoom{id: 1}
	a.teamsMemberGo(harbor, a.frontTabKey())
	if a.room != nil || a.pageShowing() || a.input.String() != "conversation draft" {
		t.Fatal("member door kept the task room or lost the conversation draft")
	}
}

func TestTeamsSharedMemberJumpUsesTheOriginatingTeam(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	a.wall.activeID = harbor
	a.entries = []entry{
		{kind: entryTeam, team: []session.TeamLine{{Team: "harbor", Thread: "000000000001", Text: "Harbor message"}}},
		{kind: entryTeam, team: []session.TeamLine{{Team: "orbit", Thread: "000000000001", Text: "Orbit message"}}},
		{kind: entryTool, tool: "team_post", detail: toolDetail{Output: `Posted to manager in "orbit" as #1.`}},
	}
	drive(t, a, runCmd(a.teamsDo(teamsTarget{act: teamsActInteractionJump, id: harbor, arg: a.frontTabKey(), opt: "000000000001"}))...)
	if a.traffic.landing.entry != 0 || a.traffic.landing.older || a.wall.activeID != harbor {
		t.Fatalf("jump landed on another team's #1: %+v", a.traffic.landing)
	}
	_ = orbit
}

func TestTeamsStartJumpRetainsOriginAfterTheOverlayChanges(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	other, err := a.teamMake("second", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.SetParent(other, harbor); err != nil {
			return err
		}
		return f.SetManager(other, a.frontTabKey())
	}); err != nil {
		t.Fatal(err)
	}
	id := "000000000001"
	a.teamsTakeInteractions(harbor, []teamstore.Entry{{ID: id, Kind: teamstore.KindStart, From: teamstore.FromManager, To: "review", Text: "Harbor brief"}})
	a.teamsTakeInteractions(other, []teamstore.Entry{{ID: id, Kind: teamstore.KindStart, From: teamstore.FromManager, To: "review", Text: "Second brief"}})
	a.entries = []entry{
		{kind: entryTool, tool: "team_start", status: toolOK, detail: toolDetail{Args: `{"handle":"review","brief":"Harbor brief"}`, Output: "Asked for a new member @review (#1)."}},
		{kind: entryTool, tool: "team_start", status: toolOK, detail: toolDetail{Args: `{"handle":"review","brief":"Second brief"}`, Output: "Asked for a new member @review (#1)."}},
	}
	a.wall.activeID = other
	if got := a.teamEntryInTeamAt(id, harbor); got != 0 {
		t.Fatalf("originating team's start landed at %d, want 0", got)
	}
}

func TestTeamsDeliveredMessageJumpSurvivesRenamingWithoutGuessing(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	id := "000000000001"
	line := session.TeamLine{Team: "harbor", Thread: id, From: teamstore.FromManager, Kind: teamstore.KindNote, Text: "Retained message"}
	root := teamstore.Entry{ID: id, From: line.From, Kind: line.Kind, Text: line.Text}
	a.teamsTakeInteractions(harbor, []teamstore.Entry{root})
	a.entries = []entry{{kind: entryTeam, team: []session.TeamLine{line}}}
	if err := a.teamRename(harbor, "renamed harbor"); err != nil {
		t.Fatal(err)
	}
	if got := a.teamEntryInTeamAt(id, harbor); got != 0 {
		t.Fatalf("renamed team's delivery landed at %d, want 0", got)
	}
	a.teamsTakeInteractions(orbit, []teamstore.Entry{root})
	if got := a.teamEntryInTeamAt(id, harbor); got != -1 {
		t.Fatalf("ambiguous historical delivery guessed entry %d", got)
	}
}

func TestTeamsReplyAuthorJumpSurvivesRenaming(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	handle, member := trafficHandle(t, a, harbor, "openrouter")
	id := "000000000002"
	a.teamsTakeInteractions(harbor, []teamstore.Entry{{ID: id, Kind: teamstore.KindNote, From: handle, To: teamstore.ToManager, Text: "The layout fits"}})
	drive(t, a, runCmd(a.trafficGo(member))...)
	a.entries = []entry{{kind: entryTool, tool: "team_post", status: toolOK, detail: toolDetail{Args: `{"to":"manager","text":"The layout fits"}`, Output: `Posted to the manager in "harbor" as #2.`}}}
	if err := a.teamRename(harbor, "renamed harbor"); err != nil {
		t.Fatal(err)
	}
	if got := a.teamEntryInTeamAt(id, harbor); got != 0 {
		t.Fatalf("renamed team's reply receipt landed at %d, want 0", got)
	}
	a.entries[0].detail.Output = `Posted to the manager in "other" as #2.`
	other, err := a.teamMake("other", a.tabList())
	if err != nil || other == "" {
		t.Fatal(err)
	}
	if got := a.teamEntryInTeamAt(id, harbor); got != -1 {
		t.Fatalf("another team's reply receipt guessed entry %d", got)
	}
}

func TestTeamsNarrowLongRailLeavesRoomForTheOverview(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 60, 16
	if err := a.teamEdit(func(f *teamstore.File) error {
		for i := 0; i < 16; i++ {
			f.Teams = append(f.Teams, teamstore.Team{ID: teamstore.NewID(), Name: fmt.Sprintf("Extra team %d", i)})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	manager := mustTeam(t, a, harbor).Manager
	a.tp.previews[manager] = teamsPreview{text: "Manager's useful update"}
	a.tp.cur = teamsRef{act: teamsActMember, id: harbor, arg: manager}
	if text := teamsFrameText(a); !strings.Contains(text, "Manager") {
		t.Fatal(text)
	}
	a.tp.cur = teamsRef{act: teamsActInteractionDown, id: harbor}
	if text := teamsFrameText(a); !strings.Contains(text, "Recent interactions") {
		t.Fatal(text)
	}
}

func teamsOverviewTraffic(t *testing.T, a *app, id string, count int) {
	t.Helper()
	var entries []teamstore.Entry
	for i := 1; i <= count; i++ {
		entries = append(entries, teamstore.Entry{ID: fmt.Sprintf("%012d", i), Kind: teamstore.KindNote,
			From: teamstore.FromManager, To: "model", Text: fmt.Sprintf("Exchange %02d: check the member cards and report the result.", i), At: a.now().Add(-time.Duration(i) * time.Minute)})
	}
	a.teamsTakeInteractions(id, entries)
}

func TestTeamsInteractionPanelScrollsIndependentlyAndKeepsItsHeader(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 160, 46
	teamsOverviewTraffic(t, a, harbor, 30)
	before := teamsFrameText(a)
	if !strings.Contains(before, "Exchange 30") {
		t.Fatal(before)
	}
	rect := a.tp.table
	drive(t, a, tea.MouseWheelMsg{X: rect.x + 4, Y: rect.y + 3, Button: tea.MouseWheelDown})
	after := teamsFrameText(a)
	if !strings.Contains(after, "Exchange 27") || strings.Contains(after, "Exchange 30") {
		t.Fatal(after)
	}
	oldLines, newLines := strings.Split(before, "\n"), strings.Split(after, "\n")
	for y := 0; y < rect.y+2; y++ {
		if oldLines[y] != newLines[y] {
			t.Fatalf("table scroll moved row %d", y)
		}
	}
	if a.tp.sel != harbor || !a.at(pageTeams) {
		t.Fatal("scroll navigated away")
	}
}

func TestTeamsInteractionPagingDoesNotRepeatTheShortLastPage(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 160, 46
	teamsOverviewTraffic(t, a, harbor, 25)
	for page, span := range []string{"1 to 10 of 25", "11 to 20 of 25", "21 to 25 of 25"} {
		text := teamsFrameText(a)
		if !strings.Contains(text, span) {
			t.Fatalf("page %d: %s", page+1, text)
		}
		if page == 2 && (strings.Contains(text, "Exchange 10") || !strings.Contains(text, "Last page")) {
			t.Fatal("last page repeated earlier interactions")
		}
		a.teamsDo(teamsTarget{act: teamsActInteractionDown, id: harbor})
	}
	if a.tp.interactionOffsets[harbor] != 20 {
		t.Fatal("last-page control moved beyond the final page")
	}
	drive(t, a, key("pgup"))
	if text := teamsFrameText(a); !strings.Contains(text, "11 to 20 of 25") {
		t.Fatal("PgUp did not return to the previous complete page")
	}
}

func TestTeamsClosedCategoryStaysInTheFooterAndItsLongListCanBeWalked(t *testing.T) {
	a, harbor, orbit := teamsHostedLab(t)
	a.width, a.height = 120, 24
	last := ""
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.Close(orbit, a.now(), ""); err != nil {
			return err
		}
		for i := 0; i < 30; i++ {
			last = teamstore.NewID()
			f.Teams = append(f.Teams, teamstore.Team{ID: last, Name: fmt.Sprintf("Closed example %02d", i)})
			if err := f.Close(last, a.now().Add(-time.Duration(i+1)*time.Hour), ""); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = teamsFrameText(a)
	for _, hit := range a.tp.targets {
		if hit.act == teamsActClosedFold && hit.y != a.height-placeFootRowsFor(pageTeams, a.height)-1 {
			t.Fatalf("Closed category is on row %d, want the sidebar footer", hit.y)
		}
	}
	a.teamsDo(teamsTarget{act: teamsActClosedFold})
	a.tp.cur = teamsRef{act: teamsActSelect, id: last}
	if text := teamsFrameText(a); !strings.Contains(text, "Closed example 29") {
		t.Fatal("long Closed category hid its last keyboard stop")
	}
	a.tp.cur = teamsRef{act: teamsActSelect, id: harbor}
	if text := teamsFrameText(a); !strings.Contains(text, "harbor") {
		t.Fatal("expanded Closed category made active teams unreachable")
	}
}

func TestTeamsManyMembersAndTheirInteractionPanelRemainReachable(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 160, 28
	last := ""
	if err := a.teamEdit(func(f *teamstore.File) error {
		for i := 0; i < 30; i++ {
			last = fmt.Sprintf("/tmp/overview-member-%02d.jsonl", i)
			if err := f.AddMember(harbor, teamstore.Member{Key: last, File: last, Word: fmt.Sprintf("Additional conversation %02d", i), Handle: fmt.Sprintf("extra%d", i)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	crew := a.teamsCrew(mustTeam(t, a, harbor))
	for _, member := range crew {
		a.tp.previews[member.key] = teamsPreview{text: "Saved member update"}
	}
	lastRow := crew[(len(crew)-1)/3*3].key
	a.tp.cur = teamsRef{act: teamsActMember, id: harbor, arg: crew[0].key}
	for i := 0; i < len(crew) && a.tp.cur.arg != lastRow; i++ {
		_ = teamsFrameText(a)
		drive(t, a, key("down"))
	}
	if a.tp.cur.arg != lastRow {
		t.Fatalf("Down could not reach the last grid row: %+v", a.tp.cur)
	}
	for i := 0; i < 3 && a.tp.cur.arg != last; i++ {
		_ = teamsFrameText(a)
		drive(t, a, key("right"))
	}
	if a.tp.cur.arg != last {
		t.Fatalf("Right could not reach the last member: %+v", a.tp.cur)
	}
	if text := teamsFrameText(a); !strings.Contains(text, "@extra29") {
		t.Fatal("last member is unreachable")
	}
	for i := 0; i < 10 && a.tp.cur.act != teamsActInteractionDown; i++ {
		_ = teamsFrameText(a)
		drive(t, a, key("down"))
	}
	if a.tp.cur.act != teamsActInteractionDown {
		t.Fatal("Down could not reach the interaction panel")
	}
	if text := teamsFrameText(a); !strings.Contains(text, "Recent interactions") {
		t.Fatal("large member grid made the interaction panel unreachable")
	}
}

func TestTeamsComposerNamesTheSelectedMembershipAndFollowsRenaming(t *testing.T) {
	a, harbor, _, _ := trafficApp(t)
	other, err := a.teamMake("second", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	manager := ""
	for _, tab := range a.tabList() {
		if tab.key != a.frontTabKey() {
			manager = tab.key
			break
		}
	}
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetManager(other, manager) }); err != nil {
		t.Fatal(err)
	}
	m, _ := mustTeam(t, a, other).Member(a.frontTabKey())
	a.wall.activeID = other
	if got := a.trafficHint(); got != "to @"+m.Handle+" of second" {
		t.Fatalf("shared manager/member composer names another team: %q", got)
	}
	if err := a.teamRename(other, "renamed second"); err != nil {
		t.Fatal(err)
	}
	if got := a.trafficHint(); got != "to @"+m.Handle+" of renamed second" {
		t.Fatalf("composer kept the old team name: %q", got)
	}
	a.wall.activeID = harbor
	if got := a.trafficHint(); got != "to "+a.teamManagerMark()+" manager of harbor" {
		t.Fatalf("manager composer does not name its team: %q", got)
	}
}

func TestTeamsDragNeverDropsOnHiddenRailRowsInTheHeadOrFooter(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 120, 24
	middle := ""
	if err := a.teamEdit(func(f *teamstore.File) error {
		for i := 0; i < 40; i++ {
			id := teamstore.NewID()
			f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: fmt.Sprintf("Long rail %02d", i)})
			if i == 20 {
				middle = id
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.tp.cur = teamsRef{act: teamsActSelect, id: middle}
	_ = teamsFrameText(a)
	a.tdrag = teamDrag{press: true, on: true, member: true, id: harbor, key: a.frontTabKey()}
	checked := 0
	for _, hit := range a.tp.targets {
		if !hit.hidden || hit.act != teamsActSelect || hit.y < 0 || hit.y >= a.height {
			continue
		}
		checked++
		if id, ok, _ := a.teamDropAt(hit.x0+1, hit.y); ok && id == hit.id {
			t.Fatalf("hidden row at y=%d can receive its own drag: %q", hit.y, id)
		}
	}
	if checked == 0 {
		t.Fatal("fixture did not place a hidden keyboard stop in the head or footer")
	}
}

func TestTeamsInteractionRowsShowKeyboardAndPointerFocus(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 160, 46
	a.pal = newPalette(tokens.TrueColor, false)
	teamsOverviewTraffic(t, a, harbor, 3)
	team := mustTeam(t, a, harbor)
	draw := func() string {
		d := &teamsDraw{a: a}
		return strings.Join(a.teamsInteractionTable(d, team, 100, 0), "\n")
	}
	a.tp.cur, a.tp.hot = teamsRef{}, teamsRef{}
	before := draw()
	ref := teamsRef{act: teamsActInteractionToggle, id: harbor, arg: "000000000003"}
	a.tp.focus, a.tp.cur = true, ref
	keyboard := draw()
	a.tp.cur, a.tp.hot = teamsRef{}, ref
	pointer := draw()
	if keyboard == before || pointer == before || plain(keyboard) != plain(before) || plain(pointer) != plain(before) {
		t.Fatal("interaction focus must be visible without changing the row's words")
	}
}

func TestTeamsInteractionExpansionAndParticipantDoors(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	a.width, a.height = 160, 46
	root := teamstore.Entry{ID: "000000000001", Kind: teamstore.KindNote, From: teamstore.FromManager, To: "model", Text: "Which layout is best?"}
	reply := teamstore.Entry{ID: "000000000002", Kind: teamstore.KindNote, From: "model", To: teamstore.ToManager, Answers: root.ID, Text: "The table keeps more exchanges visible."}
	event := teamstore.Entry{ID: "000000000003", Kind: teamstore.KindEvent, From: "model", To: teamstore.ToManager, Answers: root.ID, State: teamstore.StateFinished}
	a.teamsTakeInteractions(harbor, []teamstore.Entry{root, reply, event})
	text := teamsFrameText(a)
	if !strings.Contains(text, "1 reply") || strings.Contains(text, "2 replies") {
		t.Fatal(text)
	}
	a.teamsDo(teamsTarget{act: teamsActInteractionToggle, id: harbor, arg: root.ID})
	text = teamsFrameText(a)
	if !strings.Contains(text, "The table keeps more exchanges visible.") {
		t.Fatal(text)
	}
	team := mustTeam(t, a, harbor)
	member := teamsInteractionMember(team, "model")
	found := false
	for _, target := range a.tp.targets {
		if target.act == teamsActInteractionJump && target.arg == member && target.opt == root.ID {
			found = true
			drive(t, a, runCmd(a.teamsDo(target))...)
			break
		}
	}
	if !found || a.frontTabKey() != member || a.wall.activeID != harbor || a.at(pageTeams) {
		t.Fatal("recipient alias did not open its own chat with the originating overlay")
	}
}

func TestTeamsOverviewCacheDoesNotDuplicateOrReplayLiveTraffic(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	entry := teamstore.Entry{ID: "000000000001", Kind: teamstore.KindStart, From: teamstore.FromManager, To: "model", Text: "Historical start"}
	a.traffic.cursor = nil
	a.teamsTakeInteractions(harbor, []teamstore.Entry{entry})
	a.trafficTake([]trafficGot{{trafficJob: trafficJob{id: harbor, first: true}, entries: []teamstore.Entry{entry}}}, nil, "", a.traffic.edits, false)
	if len(a.traffic.rows[harbor]) != 1 {
		t.Fatalf("duplicate cache rows: %+v", a.traffic.rows[harbor])
	}
	if a.teamsTakeInteractions(harbor, []teamstore.Entry{entry}) {
		t.Fatal("quiet overview read requests another repaint")
	}
}

func TestTeamsOverviewDoesNotClearConversationUnread(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	member := mustTeam(t, a, harbor).Manager
	a.unreadChats = map[string]bool{member: true}
	text := teamsFrameText(a)
	if !a.unreadChats[member] || !strings.Contains(text, "unread") {
		t.Fatal("reading an overview cleared chat unread")
	}
}

func TestTeamsOverviewKeepsMembersAndInteractionsReachableAtSmallWidths(t *testing.T) {
	for _, width := range []int{40, 60, 80, 110, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, harbor, _ := teamsHostedLab(t)
			a.width, a.height = width, 24
			for _, member := range mustTeam(t, a, harbor).Members {
				a.tp.previews[member.Key] = teamsPreview{text: "A saved assistant update"}
			}
			teamsOverviewTraffic(t, a, harbor, 30)
			seen := map[string]bool{}
			a.tp.cur = teamsRef{act: teamsActMember, id: harbor, arg: mustTeam(t, a, harbor).Manager}
			for i := 0; i < 12; i++ {
				text := teamsFrameText(a)
				for _, line := range strings.Split(text, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("row exceeds %d cells: %q", width, line)
					}
				}
				for _, target := range a.tp.targets {
					if target.y >= placeHeadRows && target.y < a.height-placeBareFootRows {
						seen[target.arg] = true
					}
				}
				a.teamsWalk(0, 1)
			}
			for _, member := range mustTeam(t, a, harbor).Members {
				if !seen[member.Key] {
					t.Fatalf("member %q cannot be reached", member.Key)
				}
			}
			if !strings.Contains(teamsFrameText(a), "Recent interactions") {
				t.Fatal("cannot reach interaction table")
			}
		})
	}
}

func TestTeamsRailKeepsCreationAndHistoryVisibleWithALongTree(t *testing.T) {
	a, _, orbit := teamsHostedLab(t)
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.Close(orbit, a.now(), ""); err != nil {
			return err
		}
		for i := 0; i < 40; i++ {
			f.Teams = append(f.Teams, teamstore.Team{ID: teamstore.NewID(), Name: fmt.Sprint("long ", i)})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, height := range []int{1, 3, 10} {
		d := &teamsDraw{a: a}
		rows := a.teamsRailWindow(d, 24, height)
		if len(rows) != height {
			t.Fatal("rail exceeded its height")
		}
		newVisible, closedVisible := false, false
		for _, target := range d.targets {
			if target.act == teamsActNewTeam && !target.hidden {
				newVisible = true
			}
			if target.act == teamsActClosedFold && !target.hidden {
				closedVisible = true
			}
		}
		if !newVisible || height >= 4 && !closedVisible {
			t.Fatal("permanent sidebar controls disappeared")
		}
	}
}
