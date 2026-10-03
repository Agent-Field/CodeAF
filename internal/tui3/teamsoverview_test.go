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
	a.tp.cur = teamsRef{act: teamsActInteractionDown, id: id}
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
	for page, span := range []string{"1 to 6 of 25", "7 to 12 of 25", "13 to 18 of 25", "19 to 24 of 25", "25 to 25 of 25"} {
		text := teamsFrameText(a)
		if !strings.Contains(text, span) {
			t.Fatalf("page %d: %s", page+1, text)
		}
		if page == 4 && (strings.Contains(text, "Exchange 10") || !strings.Contains(text, "Last page")) {
			t.Fatal("last page repeated earlier interactions")
		}
		a.teamsDo(teamsTarget{act: teamsActInteractionDown, id: harbor})
	}
	if a.tp.interactionOffsets[harbor] != 24 {
		t.Fatal("last-page control moved beyond the final page")
	}
	drive(t, a, key("pgup"))
	if text := teamsFrameText(a); !strings.Contains(text, "19 to 24 of 25") {
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
	members := crew[1:]
	lastRow := members[(len(members)-1)/3*3].key
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
	for i := 0; i < len(crew)+10 && a.tp.cur.act != teamsActInteractionDown; i++ {
		_ = teamsFrameText(a)
		drive(t, a, key("up"))
	}
	if a.tp.cur.act != teamsActInteractionDown {
		t.Fatal("Up from members could not reach the interaction panel")
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
	a.tp.cur = teamsRef{act: teamsActInteractionDown, id: harbor}
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
			sawInteractions := false
			a.tp.cur = teamsRef{act: teamsActMember, id: harbor, arg: mustTeam(t, a, harbor).Manager}
			for i := 0; i < 20; i++ {
				text := teamsFrameText(a)
				sawInteractions = sawInteractions || strings.Contains(text, "Recent interactions")
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
			if !sawInteractions {
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

func TestManagerPreviewPreservesRecentAuthorsParagraphsAndBounds(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	var data strings.Builder
	for i := 0; i < teamsPreviewMessages+4; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		fmt.Fprintf(&data, "{\"type\":\"message\",\"role\":%q,\"content\":%q}\n", role, fmt.Sprintf("Message %02d\nSecond paragraph", i))
	}
	if err := os.WriteFile(file, []byte(data.String()), 0600); err != nil {
		t.Fatal(err)
	}
	p := teamsReadPreview(file, teamsPreview{})
	if p.count != teamsPreviewMessages || p.messages[0].text != "Message 04\nSecond paragraph" || p.messages[7].role != "assistant" {
		t.Fatalf("excerpt: %+v", p)
	}
	if p.text != "Message 11 Second paragraph" {
		t.Fatal("compact preview changed")
	}
}

func TestSelectedManagerCardLeadsCompactMembersAndUsesActualMessages(t *testing.T) {
	for _, width := range []int{40, 80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, id, _ := teamsHostedLab(t)
			a.width, a.height = width, 48
			team := mustTeam(t, a, id)
			manager := team.Manager
			a.entries = nil
			a.tp.previews = map[string]teamsPreview{}
			a.tp.previews[manager] = teamsPreview{count: 2, messages: [teamsPreviewMessages]teamsPreviewMessage{{role: "user", text: "Which layout?"}, {role: "assistant", text: "Use the wide layout.\nKeep the narrow fallback."}}}
			d := &teamsDraw{a: a}
			rows := a.teamsMemberCards(d, team, width, 0)
			text := plain(strings.Join(rows, "\n"))
			for _, want := range []string{"You", "Which layout?", "Use the wide layout.", "Keep the narrow fallback."} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q: %s", want, text)
				}
			}
			interactionRow := -1
			for i, row := range rows {
				if strings.Contains(plain(row), "Recent interactions") {
					interactionRow = i
					break
				}
			}
			if interactionRow != a.teamsManagerHeight()+1 {
				t.Fatal("interactions do not separate manager and members")
			}
			managerHeight := a.teamsManagerHeight()
			crew := a.teamsCrew(team)
			if len(crew) < 2 {
				t.Fatal("fixture has no ordinary member")
			}
			firstMemberY := -1
			for _, target := range d.targets {
				if target.act == teamsActMember && target.arg == crew[1].key && firstMemberY < 0 {
					firstMemberY = target.y
				}
				if target.act == teamsActMember && target.arg == manager && target.id != id {
					t.Fatal("preview has wrong overlay")
				}
			}
			if firstMemberY != managerHeight+a.teamsInteractionHeight()+7 {
				t.Fatalf("member grid starts at %d, want %d", firstMemberY, managerHeight+a.teamsInteractionHeight()+7)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatal("card exceeds pane")
				}
			}
			if a.unreadChats[manager] {
				t.Fatal("fixture unexpected unread")
			}
			if a.unreadChats == nil {
				a.unreadChats = map[string]bool{}
			}
			a.unreadChats[manager] = true
			a.teamsMemberCards(&teamsDraw{a: a}, team, width, 0)
			if !a.unreadChats[manager] {
				t.Fatal("preview consumed unread")
			}
		})
	}
}

func TestManagerPreviewUsesFrontConversationAndLabelsClippedTail(t *testing.T) {
	a, id, _ := teamsPlaceLabIDs(t)
	team := mustTeam(t, a, id)
	team.Manager = a.frontTabKey()
	a.width, a.height = 100, 40
	a.tp.previews[team.Manager] = teamsPreview{text: "stale saved update"}
	a.entries = []entry{{kind: entryUser, text: "Use this prompt"}, {kind: entryAssistant, text: strings.Repeat("earlier line\n", 30) + "Latest answer", cut: true}, {kind: entryThinking, text: "private reasoning"}}
	r := teamsCrewRow{key: team.Manager, handle: "lead", manager: true, word: "idle"}
	rows := a.teamsManagerCard(&teamsDraw{a: a}, team, r, 100, 0)
	text := plain(strings.Join(rows, "\n"))
	if !strings.Contains(text, "Latest answer") || !strings.Contains(text, "Manager (interrupted) (continued)") || strings.Contains(text, "stale saved update") || strings.Contains(text, "private reasoning") {
		t.Fatal(text)
	}
}

func TestManagerPreviewSharesChatsDeliveryAndCorrectionWords(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.height = 48
	manager := mustTeam(t, a, id).Manager
	wrapper := `Team traffic in "harbor" for you (@boss). These are the team's messages, not the person's words:
from @scrape #1: The draft is ready.
(SECRET MODEL RULE)`
	saved := session.DisplayEntry{Role: "aside", Text: wrapper}
	messages := teamsPreviewMessageOf(saved)
	if len(messages) != 1 || messages[0].text != "The draft is ready." || messages[0].author != "@scrape to @boss" {
		t.Fatalf("delivery: %+v", messages)
	}
	structured := session.DisplayEntry{Role: "aside", Team: []session.TeamLine{{From: "scrape", To: teamstore.ToManager, Text: "Ready without wrapper", Kind: teamstore.KindNote}}}
	if got := teamsPreviewMessageOf(structured); len(got) != 1 || got[0].text != "Ready without wrapper" {
		t.Fatalf("structured delivery: %+v", got)
	}
	correction := session.DisplayEntry{Role: "user", Text: "Use narrow instead", Steer: &session.SteerMark{}}
	if got := teamsPreviewMessageOf(correction); len(got) != 1 || got[0].role != "correction" {
		t.Fatalf("saved correction: %+v", got)
	}
	a.entries = []entry{{kind: entryTeam, text: wrapper}, {kind: entrySteer, steer: &steerElbow{words: "Use narrow instead"}}}
	rows := a.teamsManagerCard(&teamsDraw{a: a}, mustTeam(t, a, id), teamsCrewRow{key: manager, handle: "boss", manager: true, word: "idle"}, 120, 0)
	text := plain(strings.Join(rows, "\n"))
	for _, want := range []string{"@scrape to @boss", "The draft is ready.", "You (correction)", "Use narrow instead"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "SECRET MODEL RULE") || strings.Contains(text, teamAsideLead) {
		t.Fatal("model wrapper leaked")
	}
}

func TestManagerPreviewClickUsesTheOriginatingTeamsComposer(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	l.a.width, l.a.height = 140, 50
	l.selectOrbit(t)
	l.a.tp.previews[l.key] = teamsPreview{text: "Here is my update"}
	l.a.tp.cur = teamsRef{act: teamsActMember, id: l.orbit, arg: l.key}
	teamsFrameText(l.a)
	var preview teamsTarget
	for _, target := range l.a.tp.targets {
		if target.act == teamsActMember && target.arg == l.key && target.y >= placeHeadRows && target.y < l.a.height-placeBareFootRows {
			preview = target
		}
	}
	if preview.arg == "" {
		t.Fatal("no visible preview link")
	}
	drive(t, l.a, runCmd(l.a.teamsDo(preview))...)
	if l.a.frontTabKey() != l.key || l.a.wall.activeID != l.orbit || l.a.pageShowing() || l.a.tp.focus {
		t.Fatal("preview did not open originating team's composer")
	}
}

func TestTallManagerLatestExcerptRemainsReachableByArrowsAndWheel(t *testing.T) {
	for _, gesture := range []string{"arrows", "wheel"} {
		t.Run(gesture, func(t *testing.T) {
			a, id, _ := teamsHostedLab(t)
			a.width, a.height = 40, 24
			if err := a.teamEdit(func(f *teamstore.File) error {
				i := teamstore.Index(f.Teams, id)
				m, _ := f.Teams[i].Member(f.Teams[i].Manager)
				f.Teams[i].Members = []teamstore.Member{m}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			manager := mustTeam(t, a, id).Manager
			a.tp.previews = map[string]teamsPreview{}
			a.entries = []entry{{kind: entryAssistant, text: strings.Repeat("Older line\n", 20) + "LATEST DECISION"}}
			a.tp.cur = teamsRef{act: teamsActMember, id: id, arg: manager}
			a.tp.focus = true
			a.touch()
			before := teamsFrameText(a)
			if strings.Contains(before, "LATEST DECISION") {
				t.Fatal("fixture did not clip latest preview")
			}
			if gesture == "arrows" {
				drive(t, a, key("down"))
			} else {
				var hit teamsTarget
				for _, target := range a.tp.targets {
					if target.act == teamsActMember && target.arg == manager && target.y >= placeHeadRows && target.y < a.height-placeBareFootRows {
						hit = target
						break
					}
				}
				drive(t, a, tea.MouseWheelMsg{X: hit.x0 + 1, Y: hit.y, Button: tea.MouseWheelDown})
			}
			if text := teamsFrameText(a); !strings.Contains(text, "LATEST DECISION") {
				t.Fatalf("latest preview inaccessible via %s: %s", gesture, text)
			}
			if a.tp.cur.arg != manager || a.tp.cur.opt != "preview" || !a.at(pageTeams) {
				t.Fatal("reading moved away from manager")
			}
		})
	}
}

func TestManagerPreviewKeepsQuotedDeliveryTextAndBoundsLiveWords(t *testing.T) {
	quoted := `Team traffic in "example" for you (@boss).
from @example: text quoted by the person
(rule quoted by the person)`
	for _, role := range []string{"user", "assistant"} {
		got := teamsPreviewMessageOf(session.DisplayEntry{Role: role, Text: quoted})
		if len(got) != 1 || got[0].role != role || got[0].text != quoted {
			t.Fatalf("quoted %s delivery reinterpreted: %+v", role, got)
		}
	}
	a, _, _ := teamsHostedLab(t)
	a.entries = []entry{{kind: entryUser, text: "Earlier prompt"}, {kind: entryAssistant, text: strings.Repeat("界", teamsPreviewBytes) + "Newest"}}
	got := a.teamsManagerMessages(a.frontTabKey())
	n := 0
	for _, m := range got {
		n += len(m.text)
		if !strings.Contains(m.text, "Newest") {
			t.Fatal("lost newest message")
		}
	}
	if len(got) != 1 || n > teamsPreviewBytes {
		t.Fatalf("live excerpt exceeded shared bound: %d bytes, %d messages", n, len(got))
	}
}

func TestManagerReadingWheelContinuesToMembersAndInteractions(t *testing.T) {
	a, id, _ := teamsHostedLab(t)
	a.width, a.height = 80, 24
	manager := mustTeam(t, a, id).Manager
	a.tp.previews = map[string]teamsPreview{}
	a.entries = []entry{{kind: entryAssistant, text: strings.Repeat("Older line\n", 20) + "LATEST WORDS"}}
	a.tp.focus = true
	a.tp.cur = teamsRef{act: teamsActMember, id: id, arg: manager}
	a.touch()
	teamsFrameText(a)

	var pointer teamsTarget
	for _, target := range a.tp.targets {
		if target.act == teamsActMember && target.arg == manager && target.y >= placeHeadRows && target.y < a.height-placeBareFootRows {
			pointer = target
			break
		}
	}
	if pointer.arg == "" {
		t.Fatal("no visible manager reading target")
	}
	wheel := func() {
		drive(t, a, tea.MouseWheelMsg{X: pointer.x0 + 1, Y: pointer.y, Button: tea.MouseWheelDown})
		teamsFrameText(a)
	}
	wheel()
	if a.tp.cur.opt != "preview" {
		t.Fatal("first wheel missed latest preview")
	}
	wheel()
	if a.tp.cur.act == teamsActMember && a.tp.cur.arg == manager {
		t.Fatal("second wheel trapped in manager")
	}
	// Once the pointer reaches the interaction table, wheel ticks scroll that
	// panel's own content. Move it into the pane's free column to continue down.
	sawInteractions := strings.Contains(teamsFrameText(a), "Recent interactions")
	for i := 0; i < 10; i++ {
		drive(t, a, tea.MouseWheelMsg{X: a.tp.table.x + 1, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
		teamsFrameText(a)
		sawInteractions = sawInteractions || strings.Contains(teamsFrameText(a), "Recent interactions")
		if a.tp.cur.act == teamsActMember && a.tp.cur.arg != manager {
			if !sawInteractions {
				t.Fatal("wheel skipped interactions")
			}
			return
		}
	}
	t.Fatal("wheel could not continue to members")

}

func TestBoxedTeamDecisionKeepsAnswerHitInsideItsBorder(t *testing.T) {
	for _, width := range []int{24, 40, 80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, id, _ := teamsPlaceLabIDs(t)
			a.width, a.height = width, 100
			flushTeams(t, a)
			p, err := a.teamsSeam().Raise(teamstore.Packet{Team: teamstore.Person, Origin: id, Kind: teamstore.PacketQuestion, RaisedBy: teamstore.FromManager, Question: "Which layout should we review first?", Options: []teamstore.Option{{ID: "wide", Label: "Wide terminal", Consequence: "Review the wide layout"}, {ID: "narrow", Label: "Narrow terminal", Consequence: "Review the narrow layout"}}})
			if err != nil {
				t.Fatal(err)
			}
			drive(t, a, runCmd(a.teamsRead(false))...)
			d := &teamsDraw{a: a}
			rows := a.teamsCard(d, p, width, 8)
			if len(rows) < 4 || !strings.HasPrefix(plain(rows[0]), "╭") || !strings.HasPrefix(plain(rows[len(rows)-1]), "╰") {
				t.Fatal("decision lost boundary")
			}
			a.tp.targets = d.targets
			var chosen teamsTarget
			for _, target := range d.targets {
				if target.x0 < 2 || target.x1 > width-2 || target.y <= 8 || target.y >= 8+len(rows)-1 {
					t.Fatalf("answer touches border: %+v", target)
				}
				if target.act == teamsActOption && target.opt == "narrow" {
					chosen = target
				}
			}
			if chosen.opt == "" {
				t.Fatal("missing narrow choice")
			}
			if !strings.Contains(ansi.Cut(plain(rows[chosen.y-8]), chosen.x0, chosen.x1), "Narrow") {
				t.Fatal("answer hit does not lie on its label")
			}
			if _, ok := a.teamsTargetAt(0, chosen.y); ok {
				t.Fatal("border has an answer hit")
			}
			hit, ok := a.teamsTargetAt(chosen.x0+1, chosen.y)
			if !ok || hit.opt != "narrow" {
				t.Fatal("clicked label did not resolve its answer")
			}
			drive(t, a, runCmd(a.teamsDo(hit))...)
			packets, _, _, err := a.teamsSeam().Packets(teamstore.ScopeAll, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, packet := range packets {
				if packet.ID == p.ID && packet.Waiting() {
					t.Fatal("answer did not decide the boxed question")
				}
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != width {
					t.Fatal("question box exceeds pane")
				}
			}
		})
	}
}

func TestBoxedPermissionPromptRetainsWrappedChoicesAndSendsAnswer(t *testing.T) {
	for _, width := range []int{24, 40, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, id, _ := teamsPlaceLabIDs(t)
			a.width, a.height = width, 100
			team := mustTeam(t, a, id)
			m := team.Members[0]
			for _, member := range team.Members {
				if member.File != a.file {
					m = member
					break
				}
			}
			now := time.Now()
			a.clock = func() time.Time { return now }
			question := consentQuestion(17, "Allow this member to regenerate the report?")
			row := session.SessionRow{Transcript: m.File, Dir: filepath.Dir(m.File), Live: true, Presence: session.SessionPresence{UpdatedAt: now, State: session.PresenceWaiting, Question: question}}
			a.tp.world = map[string]session.SessionRow{filepath.Clean(m.File): row}
			var sent []string
			a.leaveAnswer = func(dir string, kind session.QuestionKind, id uint64, key string) error {
				sent = append(sent, key)
				return nil
			}
			d := &teamsDraw{a: a}
			rows := a.teamsPromptRows(d, team, width, 8)
			keys := map[string]bool{}
			a.tp.targets = d.targets
			for _, target := range d.targets {
				if target.act != teamsActPrompt {
					continue
				}
				keys[target.opt] = true
				if target.x0 < 2 || target.x1 > width-2 {
					t.Fatal("prompt answer crosses border")
				}
				if !strings.Contains(plain(rows[target.y-8]), question.Label(target.opt)) {
					t.Fatalf("choice is hidden: %s", target.opt)
				}
			}
			for _, chip := range answerChips(question) {
				if !keys[chip.key] {
					t.Fatalf("lost wrapped choice: %s", chip.label)
				}
			}
			var selected teamsTarget
			for _, target := range d.targets {
				if target.act == teamsActPrompt {
					selected = target
					break
				}
			}
			hit, ok := a.teamsTargetAt(selected.x0+1, selected.y)
			if !ok {
				t.Fatal("boxed permission choice is not clickable")
			}
			drive(t, a, runCmd(a.teamsDo(hit))...)
			if len(sent) != 1 || sent[0] != selected.opt {
				t.Fatalf("prompt chose wrong answer: %+v", sent)
			}
			for _, line := range rows {
				if ansi.StringWidth(line) > width {
					t.Fatal("prompt exceeds box")
				}
			}
		})
	}
}
