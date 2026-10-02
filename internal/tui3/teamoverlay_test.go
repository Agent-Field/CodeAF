package tui3

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestTeamOverlayShowsAllMembersWithoutCloseTargetsOrAnUnrelatedTab(t *testing.T) {
	a, harbor, _ := menuApp(t)
	front := a.frontTabKey()
	if err := a.teamMakeManager(harbor, a.teamMenuFront()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 35; i++ {
		if err := a.teamAdd(harbor, []chatTab{{key: fmt.Sprintf("saved%d", i), file: fmt.Sprintf("/tmp/saved%d/transcript.jsonl", i), where: "/tmp", word: fmt.Sprintf("saved conversation %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	a.tabShutKey(front)
	tabs := a.teamStripTabs(a.tabList())
	if len(tabs) != 37 || tabs[0].key != front {
		t.Fatalf("overlay tabs: %d, first %+v", len(tabs), tabs[0])
	}
	for _, tab := range tabs {
		if tab.team != harbor {
			t.Fatal("membership tab has no overlay identity")
		}
	}
	_ = a.tabsRow(a.width)
	for _, hit := range a.chatTabHits {
		if hit.kind == tabClose {
			t.Fatal("overlay exposes a tab close action")
		}
	}
	drive(t, a, key("ctrl+w"))
	if a.frontTabKey() != front || a.closingTab() {
		t.Fatal("ctrl+w changed the overlay's conversation")
	}
	a.teamViewSet("")
	if a.trafficHint() != "" || a.sideKind() != sideKindPlain {
		t.Fatal("All inferred a team overlay")
	}
}

func TestTeamOverlayRemembersEachSelectionAndSharesDraftsAcrossViews(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	first := a.frontTabKey()
	a.input.insert("shared conversation draft")
	a.tabView = tabViewport{from: 3, browsing: true}
	_ = a.teamActivate(orbit)
	if a.frontTabKey() == first {
		t.Fatal("orbit did not select its member")
	}
	a.input.insert("orbit draft")
	_ = a.teamActivate(harbor)
	if a.frontTabKey() != first || a.input.String() != "shared conversation draft" || a.tabView.from != 3 {
		t.Fatal("overlay selection, draft or viewport was not restored")
	}
	_ = a.teamActivate("")
	if a.frontTabKey() != first || a.input.String() != "shared conversation draft" {
		t.Fatal("All lost the shared conversation draft")
	}
	_ = a.teamActivate(orbit)
	if a.input.String() != "orbit draft" {
		t.Fatal("the second view lost its draft")
	}
}

func TestTeamOverlayBadgeActivatesTheCurrentConversationAndHomeReturnsToAll(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	if err := a.teamAdd(orbit, []chatTab{a.teamMenuFront()}); err != nil {
		t.Fatal(err)
	}
	a.teamViewSet("")
	front := a.frontTabKey()
	_ = a.teamBadgesRow(a.width)
	if len(a.teamViews.badges) != 2 {
		t.Fatal("shared conversation is missing a team badge")
	}
	badge := a.teamViews.badges[1]
	if _, took := a.teamBadgePress(badge.span.from, chatHeadRows-1); !took || a.wall.activeID != orbit || a.frontTabKey() != front {
		t.Fatal("badge did not keep the current conversation")
	}
	_ = a.homeOpenLine(homeLine{row: session.SessionRow{Transcript: a.file}})
	if a.wall.activeID != "" || a.teamViews.id != "" {
		t.Fatal("Home retained a team overlay")
	}
	if !teamHolds(mustTeam(t, a, harbor), front) || !teamHolds(mustTeam(t, a, orbit), front) {
		t.Fatal("changing view changed membership")
	}
}

func TestTeamOverlayRemovalSelectsTheNearestSurvivorWithoutStoppingTheConversation(t *testing.T) {
	a, harbor, _ := menuApp(t)
	front := a.frontTabKey()
	a.teamOverlaySync()
	if err := a.teamRemove(harbor, []string{front}); err != nil {
		t.Fatal(err)
	}
	_ = a.teamOverlaySync()
	if a.frontTabKey() == front || !teamHolds(mustTeam(t, a, harbor), a.frontTabKey()) {
		t.Fatal("removed selection was not replaced")
	}
	if a.behind[front] == nil {
		t.Fatal("removal discarded the conversation")
	}
}

func TestTeamMembershipPickerIncludesMoreThanTheChatsMenuAndAddsOnlyToItsTeam(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	a.showPage(pageTeams)
	a.tp.sel = harbor
	var sessions []session.SessionRow
	for i := 0; i < 45; i++ {
		sessions = append(sessions, session.SessionRow{Transcript: fmt.Sprintf("/tmp/picker/%02d/transcript.jsonl", i), Title: fmt.Sprintf("Candidate %02d", i), ProjectDir: "/tmp/picker"})
	}
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{Sessions: sessions}}}, true
	}
	cmd := a.teamMembershipOpen(harbor, "")
	// The ordinary update cycle folds the captured reader.
	drive(t, a, cmd())
	if len(a.teamMembershipRows()) < 45 {
		t.Fatal("picker truncated the existing sessions")
	}
	a.tmembers.filter.insert("Candidate 44")
	rows := a.teamMembershipRows()
	if len(rows) != 1 {
		t.Fatal("filter did not find the final candidate")
	}
	_ = a.teamMembershipChoose(1)
	if !teamHolds(mustTeam(t, a, harbor), rows[0].key) || teamHolds(mustTeam(t, a, orbit), rows[0].key) {
		t.Fatal("addition changed the wrong memberships")
	}
}

func TestTeamMembershipControlsRemainVisibleAndDoNotOverlapConversationLinks(t *testing.T) {
	a, harbor, _ := teamsHostedLab(t)
	for _, width := range []int{40, 72, 100, 160} {
		a.width, a.height = width, 42
		text := teamsFrameText(a)
		if !strings.Contains(text, "+ Add member") || !strings.Contains(text, "Actions") {
			t.Fatalf("membership controls missing at %d:\n%s", width, text)
		}
		for _, action := range a.tp.targets {
			if action.act != teamsActMemberActions {
				continue
			}
			for _, link := range a.tp.targets {
				if link.act == teamsActMember && action.y == link.y && action.x0 < link.x1 && link.x0 < action.x1 {
					t.Fatal("Actions overlaps a conversation link")
				}
			}
		}
	}
	manager := mustTeam(t, a, harbor).Manager
	a.teamMembershipOpen(harbor, manager)
	a.teamMembershipChoose(0)
	if !teamHolds(mustTeam(t, a, harbor), manager) || !a.tmembers.on {
		t.Fatal("removing the manager did not require a replacement")
	}
}

func TestTeamMembershipRemovalKeepsOtherMemberships(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	member := a.frontTabKey()
	if err := a.teamAdd(orbit, []chatTab{a.teamMenuFront()}); err != nil {
		t.Fatal(err)
	}
	a.teamMembershipOpen(harbor, member)
	a.teamMembershipChoose(0)
	if teamHolds(mustTeam(t, a, harbor), member) || !teamHolds(mustTeam(t, a, orbit), member) {
		t.Fatal("removing one membership changed another")
	}
}

func TestTeamMembershipNewConversationWaitsForItsStoredMembershipBeforeSubmitting(t *testing.T) {
	a, harbor, _ := menuApp(t)
	a.showPage(pageTeams)
	where := t.TempDir()
	file := where + "/new/transcript.jsonl"
	next := &membershipSubmitAgent{fakeAgent: &fakeAgent{}, profile: a.profileDir, team: harbor, file: file}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, Workspace: where, SessionFile: file}, nil
	}
	a.teamMembershipOpen(harbor, "")
	a.tmembers.new = true
	a.tmembers.filter.insert("Build the membership editor")
	cmd := a.teamMembershipStart()
	drive(t, a, cmd())
	if !next.stored || len(next.sent) != 1 || next.sent[0] != "Build the membership editor" {
		t.Fatalf("first assignment: %v", next.sent)
	}
	stored, err := teamstore.Load(a.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := stored.Team(harbor)
	if !target.Holds(a.frontTabKey()) || a.wall.activeID != harbor || a.tmembers.on || a.pageShowing() {
		t.Fatal("new member did not land in its saved overlay")
	}
}

func TestTeamMembershipCancellingAnOpeningDiscardsItsLateResult(t *testing.T) {
	a, harbor, _ := menuApp(t)
	a.showPage(pageTeams)
	next := &fakeAgent{}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, Workspace: t.TempDir(), SessionFile: "/tmp/cancelled/transcript.jsonl"}, nil
	}
	a.teamMembershipOpen(harbor, "")
	a.tmembers.new = true
	a.tmembers.filter.insert("Cancelled assignment")
	front := a.frontTabKey()
	cmd := a.teamMembershipStart()
	a.teamMembershipShut()
	drive(t, a, cmd())
	if a.frontTabKey() != front || len(next.sent) != 0 || a.conversationOpening {
		t.Fatal("cancelled opening changed the conversation or submitted")
	}
}

func TestTeamMembershipRemovalRechecksTheManagerAtTheStore(t *testing.T) {
	a, harbor, _ := menuApp(t)
	target := a.frontTabKey()
	a.teamMembershipOpen(harbor, target)
	a.teamMembershipChoose(0)
	change := a.teamsDisk.queue[len(a.teamsDisk.queue)-1]
	fresh := &teamstore.File{Teams: []team{{ID: harbor, Manager: target, Members: []teamMember{{Key: target}}}}}
	if err := change(fresh); err == nil || !fresh.Teams[0].Holds(target) {
		t.Fatal("fresh manager was removed without replacement")
	}
}

func TestTeamOverlayRestoresStripPositionThroughRendering(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	for i := 0; i < 30; i++ {
		a.teamAdd(harbor, []chatTab{{key: fmt.Sprint("scroll", i), file: fmt.Sprintf("/tmp/scroll/%d/transcript.jsonl", i), where: "/tmp", word: fmt.Sprint("scroll", i)}})
	}
	a.width = 72
	a.tabsRow(a.width)
	a.tabView.from, a.tabView.browsing = 15, true
	a.teamActivate(orbit)
	a.teamActivate(harbor)
	a.tabsRow(a.width)
	if a.tabView.from != 15 || !a.tabView.browsing {
		t.Fatalf("strip position lost after frame: %+v", a.tabView)
	}
	for _, tab := range a.dockTabs() {
		if !teamHolds(mustTeam(t, a, harbor), tab.key) {
			t.Fatal("dock exposes an unrelated conversation")
		}
	}
}

func TestTeamOverlayEmptyMembershipReturnsToAllAndFailedOpenKeepsTheDraft(t *testing.T) {
	a, _, orbit := menuApp(t)
	a.teamActivate(orbit)
	front := a.frontTabKey()
	a.teamRemove(orbit, []string{front})
	a.teamOverlaySync()
	if a.wall.activeID != "" || a.frontTabKey() != front {
		t.Fatal("empty membership view did not return to All")
	}
	a.input.insert("keep my draft")
	a.teamViewSet(orbit)
	a.teamAdd(orbit, []chatTab{{key: "missing", file: "/absent/transcript.jsonl", where: "/absent", word: "Missing"}})
	a.teamOverlaySync()
	if a.wall.activeID != "" || a.frontTabKey() != front || a.input.String() != "keep my draft" {
		t.Fatal("failed opening retained a misleading overlay or changed draft")
	}
}

func TestTeamOverlayLongNamesOfferAnOverflowDoor(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	a.teamAdd(orbit, []chatTab{a.teamMenuFront()})
	a.teamRename(harbor, strings.Repeat("Long team name ", 10))
	a.teamMake("Third membership", []chatTab{a.teamMenuFront()})
	text := a.teamBadgesRow(30)
	if len(a.teamViews.badges) != 2 || !strings.Contains(plain(text), "+2 teams") {
		t.Fatalf("narrow badges: %q %+v", text, a.teamViews.badges)
	}
	last := a.teamViews.badges[1]
	a.teamBadgePress(last.span.from, chatHeadRows-1)
	if !a.teamMenu.on {
		t.Fatal("overflow does not lead to hidden memberships")
	}
}

// The first turn inspects the actual store, rather than a later frame's copy.
type membershipSubmitAgent struct {
	*fakeAgent
	profile, team, file string
	stored              bool
}

func (m *membershipSubmitAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	f, err := teamstore.Load(m.profile)
	if err == nil {
		target, _ := f.Team(m.team)
		m.stored = target.Holds(m.file)
	}
	return m.fakeAgent.Submit(ctx, text)
}

func TestTeamMembershipSharedCreationJoinsOnlyTheSelectedOverviewTeam(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	a.shared = true
	a.showPage(pageTeams)
	next := &fakeAgent{}
	where := t.TempDir()
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: next, Workspace: where, SessionFile: where + "/new/transcript.jsonl"}, nil
	}
	a.teamMembershipOpen(orbit, "")
	a.tmembers.new = true
	a.tmembers.filter.insert("Create for orbit only")
	cmd := a.teamMembershipStart()
	if cmd != nil {
		drive(t, a, cmd())
	}
	front := a.frontTabKey()
	if !teamHolds(mustTeam(t, a, orbit), front) || teamHolds(mustTeam(t, a, harbor), front) {
		t.Fatal("creation joined the previous overlay's team")
	}
}

func TestTeamMembershipReportingAssignmentSurvivesTheSheetClosing(t *testing.T) {
	a, harbor, _ := menuApp(t)
	member := a.frontTabKey()
	other := a.teamStripTabs(a.tabList())[2]
	a.teamMakeManager(harbor, other)
	a.teamMembershipOpen(harbor, member)
	a.teamMembershipChoose(2)
	change := a.teamsDisk.queue[len(a.teamsDisk.queue)-1]
	f := &teamstore.File{Teams: teamsClone(a.wall.teams)}
	if err := change(f); err != nil {
		t.Fatal(err)
	}
	if home, ok := f.Home(member); !ok || home.Team != harbor {
		t.Fatal("reporting assignment used cleared sheet state")
	}
}

func TestTeamMembershipNavigationBeforeWriteSettlementKeepsTheAssignmentAsADraft(t *testing.T) {
	a, harbor, _ := menuApp(t)
	a.input.setText("wait for membership")
	a.tmemberStart = teamMemberStart{key: a.frontTabKey(), prompt: a.input.String(), team: harbor, said: teamWriteSaid{seq: 99, pending: true}}
	drive(t, a, key("enter"))
	if a.input.String() != "wait for membership" {
		t.Fatal("enter lost the assignment before membership was saved")
	}
	a.showPage(pageHome)
	a.tmemberStart.said.pending = false
	if cmd := a.teamMembershipSubmit(); cmd != nil || a.input.String() != "wait for membership" {
		t.Fatal("navigation submitted instead of retaining the draft")
	}
}

func TestTeamOverlayRootUsesCurrentManagersAndProtectsAutomaticMemberships(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	former := a.frontTabKey()
	harborTeam := mustTeam(t, a, harbor)
	replacement := harborTeam.Members[1].Key
	global := mustTeam(t, a, orbit).Members[0].Key
	var root string
	if err := a.teamEdit(func(f *teamstore.File) error {
		if err := f.SetManager(harbor, former); err != nil {
			return err
		}
		root = f.MakeRoot(a.now())
		if err := f.SetManager(root, global); err != nil {
			return err
		}
		return f.AddMember(root, teamMember{Key: former, File: a.file, Where: a.workspace})
	}); err != nil {
		t.Fatal(err)
	}
	a.teamViewSet(root)
	a.teamOverlaySync()
	for _, row := range a.teamMenuRows() {
		if row.code == teamMenuToggle {
			t.Fatal("root offers ordinary membership editing")
		}
	}
	if err := a.teamRemove(root, []string{former}); err == nil {
		t.Fatal("automatic root membership can be removed")
	}
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetManager(harbor, replacement) }); err != nil {
		t.Fatal(err)
	}
	a.teamOverlaySync()
	if a.frontTabKey() == former || !a.teamOverlayHolds(mustTeam(t, a, root), a.frontTabKey()) || a.teamOverlayHolds(mustTeam(t, a, root), former) {
		t.Fatal("root retained its hidden former-manager selection")
	}
}
