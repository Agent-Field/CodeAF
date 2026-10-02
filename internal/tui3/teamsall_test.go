package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

func TestTeamsAllOverviewNestsAndNavigatesWithoutChangingChats(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	a.width, a.height = 160, 60
	front := a.frontTabKey()
	draft := "keep my draft"
	a.input.insert(draft)
	drive(t, a, runCmd(a.teamsSelect(teamsAllRow))...)
	text := teamsFrameText(a)
	if !strings.Contains(text, "Subteams") || !strings.Contains(text, "Organize") || a.frontTabKey() != front || a.input.String() != draft {
		t.Fatal(text)
	}
	var parent, child teamsTarget
	for _, tg := range a.tp.targets {
		if tg.arg != "overview" {
			continue
		}
		if tg.id == harbor && parent.id == "" {
			parent = tg
		}
		if tg.id == orbit && child.id == "" {
			child = tg
		}
	}
	if child.x0 <= parent.x0 || child.y <= parent.y || child.x1 >= parent.x1 {
		t.Fatalf("child not nested: parent %+v child %+v", parent, child)
	}
	drive(t, a, tea.MouseClickMsg{X: child.x0, Y: child.y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: child.x0, Y: child.y, Button: tea.MouseLeft})
	if a.tp.sel != orbit || !a.at(pageTeams) || a.frontTabKey() != front {
		t.Fatal("child click did not select its overview")
	}
}

func TestTeamsAllOverviewManagerLinkUsesItsTeam(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	a := l.a
	a.width, a.height = 150, 60
	drive(t, a, runCmd(a.teamsSelect(teamsAllRow))...)
	tg := teamsTargetOf(t, a, teamsActMember, l.orbit)
	drive(t, a, tea.MouseClickMsg{X: tg.x0, Y: tg.y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: tg.x0, Y: tg.y, Button: tea.MouseLeft})
	if a.at(pageTeams) || a.frontTabKey() != l.key || a.teamViews.id != l.orbit {
		t.Fatal("manager alias did not open originating team chat")
	}
}

func TestTeamsAllOverviewReadsManagersAndEverySpendPool(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	a := l.a
	drive(t, a, runCmd(a.teamsSelect(teamsAllRow))...)
	if _, ok := a.tp.previews[l.key]; !ok {
		t.Fatal("All teams did not read the child manager preview")
	}
	for _, id := range []string{l.harbor, l.orbit} {
		if _, ok := a.tp.spend[id]; !ok {
			t.Fatalf("All teams did not read spend for %s", id)
		}
	}
}

func TestTeamsAllOverviewKeepsGlobalManagerAndDecisions(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	a.width, a.height = 160, 60
	root := team{ID: "root", Name: teamstore.RootName, Root: true, Manager: a.frontTabKey(), Members: []teamMember{{Key: a.frontTabKey(), Word: "Global manager", Handle: "global"}}}
	a.wall.teams = append(a.wall.teams, root)
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == harbor {
			a.wall.teams[i].Parent = root.ID
		}
	}
	a.tp.packets = []teamstore.Packet{{ID: "question", Team: teamstore.Person, Origin: harbor, Kind: teamstore.PacketQuestion, State: teamstore.PacketOpen, Question: "Which layout first?"}}
	a.tp.packets = append(a.tp.packets, teamstore.Packet{ID: "global-question", Team: root.ID, Origin: harbor, Kind: teamstore.PacketQuestion, State: teamstore.PacketOpen, Question: "Global decision"})
	a.tp.sel = teamsAllRow
	text := teamsFrameText(a)
	for _, want := range []string{"@global", "Settings", "Choose manager", "+ Add member", "Which layout first?", "Global decision", "Subteams"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s:\n%s", want, text)
		}
	}
	if a.tp.sel != teamsAllRow {
		t.Fatal("root displaced All teams selection")
	}
}

func TestTeamsAllOverviewLongHierarchyStaysReachableAtNarrowWidths(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	for i := 0; i < 20; i++ {
		a.wall.teams = append(a.wall.teams, team{ID: fmt.Sprintf("extra-%d", i), Name: fmt.Sprintf("Team %02d", i), Made: time.Now()})
	}
	for _, width := range []int{42, 90, 160} {
		a.width, a.height = width, 22
		a.tp.sel = teamsAllRow
		_ = teamsFrameText(a)
		var last teamsRef
		for _, tg := range a.tp.targets {
			if tg.id == "extra-19" && tg.arg == "overview" {
				last = tg.ref()
				break
			}
		}
		if last.id == "" {
			t.Fatalf("last card unreachable at %d", width)
		}
		a.tp.cur = last
		text := teamsFrameText(a)
		if !strings.Contains(text, "Team 19") {
			t.Fatalf("last keyboard card not revealed at %d:\n%s", width, text)
		}
		for _, line := range strings.Split(text, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("row exceeds %d columns", width)
			}
		}
		a.tp.sel = harbor
		_ = orbit
	}
}

func TestTeamsClosedAncestryIncludesEveryLevelAndWraps(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	a.width, a.height = 100, 40
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == orbit {
			a.wall.teams[i].ClosedAt = time.Now()
			a.wall.teams[i].State = teamstore.TeamClosed
		}
	}
	a.wall.teams = append(a.wall.teams, team{ID: "deep", Name: "identical child", Parent: orbit, State: teamstore.TeamClosed, ClosedAt: time.Now()})
	a.tp.closedOpen = true
	tm, _ := a.teamByID("deep")
	if got := a.teamsAncestryName(tm); got != "harbor › orbit › identical child" {
		t.Fatal(got)
	}
	text := teamsFrameText(a)
	if !strings.Contains(text, "harbor") || !strings.Contains(text, "identical child") {
		t.Fatal(text)
	}
	for _, tg := range a.tp.targets {
		if tg.id == "deep" && !strings.Contains(tg.hint, "harbor › orbit › identical child") {
			t.Fatal(tg.hint)
		}
	}
	_ = harbor
}

func TestTeamsOrganizeStaysOnTeamsAndTinyModalCannotApply(t *testing.T) {
	a, _, _ := teamsPlaceLabIDs(t)
	a.width, a.height = 120, 40
	teamID := a.teamViews.id
	drive(t, a, runCmd(a.teamsDo(teamsTarget{act: teamsActOrganize}))...)
	if !a.at(pageTeams) || a.wall.on || !a.wall.org.on || a.teamViews.id != teamID {
		t.Fatal("Organize opened Chats or changed its overlay")
	}
	text := teamsFrameText(a)
	if a.tp.orgRect.w() == 0 || !strings.Contains(text, "Organize") {
		t.Fatal(text)
	}
	a.wall.org.props = []orgProp{{name: "hidden new team", keys: []string{a.frontTabKey()}, take: true}}
	a.width, a.height = 20, 8
	_ = teamsFrameText(a)
	before := len(a.wall.teams)
	drive(t, a, key("enter"))
	if len(a.wall.teams) != before || !a.wall.org.on {
		t.Fatal("hidden modal applied")
	}
	drive(t, a, key("esc"))
	if a.wall.org.on || !a.at(pageTeams) {
		t.Fatal("Escape did not return to Teams")
	}
}

func TestTeamsAllOverviewRefreshesFailureWithoutVisitingTheTeam(t *testing.T) {
	l := newTeamsOpenLab(t, true)
	a := l.a
	teamsFlush(t, a)
	for _, state := range []string{teamstore.StateFailed, teamstore.StateFinished} {
		if err := teamstore.AppendTraffic(a.profileDir, l.orbit, teamstore.Entry{Kind: teamstore.KindEvent, Member: l.key, From: "boss", To: teamstore.ToManager, State: state}); err != nil {
			t.Fatal(err)
		}
		drive(t, a, runCmd(a.teamsSelect(teamsAllRow))...)
		text := teamsFrameText(a)
		if strings.Contains(text, "1 failed") != (state == teamstore.StateFailed) {
			t.Fatalf("state %s not reflected in All teams:\n%s", state, text)
		}
	}
}

func TestTeamsOrganizeFirstApplyOffersOneWorkingUndoOnTeams(t *testing.T) {
	a := organizeApp(t, nil)
	a.width, a.height = 130, 45
	drive(t, a, runCmd(a.wallOrganizeOpen())...)
	_ = teamsFrameText(a)
	drive(t, a, key("enter"))
	teamsFlush(t, a)
	if len(a.wall.teams) == 0 || !a.at(pageTeams) {
		t.Fatal("Apply did not create its team on Teams")
	}
	// A refused save must retain the same undo door as a successful save.
	a.wall.org.said = teamWriteSaid{why: "not saved"}
	_ = teamsFrameText(a)
	count := 0
	var undo teamsTarget
	for _, tg := range a.tp.targets {
		if tg.act == teamsActOrganizeUndo {
			count++
			undo = tg
		}
	}
	if count != 1 {
		t.Fatalf("%d Undo targets", count)
	}
	drive(t, a, runCmd(a.teamsDo(undo))...)
	teamsFlush(t, a)
	if len(a.wall.teams) != 0 || !a.at(pageTeams) {
		t.Fatal("Undo did not restore the initially empty list")
	}
	text := teamsFrameText(a)
	if strings.Contains(text, "Undo") {
		t.Fatal(text)
	}
}
