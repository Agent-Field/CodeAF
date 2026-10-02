package tui3

import (
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/charmbracelet/x/ansi"
)

func TestConversationDeleteOnlyFrontReturnsHomeWithoutRequiringStart(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 100, 30
	// This deletion door stands for an engine that has already stopped the owner.
	a.deleteConversation = func(string, map[string]string, map[string][]string) error { return nil }
	a.start, a.fresh = nil, nil
	a.behind = nil
	file := a.file
	a.wall.teams = nil
	a.conversationDeleteOpen(file, "front")
	cmd := a.conversationDeleteChoose(len(a.conversationDeleteOptions()) - 1)
	drain(t, a, cmd)
	if a.file != "" || a.agent != nil || !a.at(pageHome) || a.cdelete.on {
		t.Fatalf("deleted front retained: file=%s agent=%v page=%v", a.file, a.agent, a.page)
	}
}

func TestConversationDeleteRequiresVisibleConfirmationAndScrollableAffectedNames(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 50, 24
	manager := a.frontTabKey()
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == parent {
			a.wall.teams[i].Manager = manager
		}
	}
	for i := 0; i < 60; i++ {
		a.wall.teams = append(a.wall.teams, team{ID: fmt.Sprintf("%012x", 1000+i), Name: fmt.Sprintf("descendant-%02d", i), Parent: parent})
	}
	a.conversationDeleteOpen(a.file, "manager")
	a.conversationDeleteChoose(1)
	for i, o := range a.conversationDeleteOptions() {
		if o.team == parent && o.replacement == "" {
			a.cdelete.cursor = i
			break
		}
	}
	a.conversationDeleteOver(strings.Repeat("\n", 24))
	if a.cdelete.detailsMax == 0 {
		t.Fatal("affected names cannot scroll")
	}
	a.cdelete.detailsTop = a.cdelete.detailsMax
	text := ansi.Strip(a.conversationDeleteOver(strings.Repeat("\n", 24)))
	if !strings.Contains(text, "descendant-59") {
		t.Fatal("last descendant cannot be reviewed")
	}
	a.width = 10
	if cmd := a.conversationDeleteChoose(len(a.conversationDeleteOptions()) - 1); cmd != nil || a.cdelete.busy {
		t.Fatal("invisible confirmation deleted")
	}
}

func TestTeamsLifecycleConfirmationScrollsLargeScopeAndRefusesChangedTree(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 50, 24
	for i := 0; i < 60; i++ {
		a.wall.teams = append(a.wall.teams, team{ID: fmt.Sprintf("%012x", 1000+i), Name: fmt.Sprintf("descendant-%02d", i), Parent: parent})
	}
	a.teamSheetOpen(parent, teamSheetClose)
	a.teamSheetCard(a.width, a.height)
	if a.tsheet.detailsMax == 0 {
		t.Fatal("large disband confirmation vanished")
	}
	a.tsheet.detailsTop = a.tsheet.detailsMax
	card := a.teamSheetCard(a.width, a.height)
	if !strings.Contains(ansi.Strip(strings.Join(card.rows, "\n")), "descendant-59") {
		t.Fatal("last descendant not visible")
	}
	a.width = 10
	if cmd := a.teamSheetDo(tsCloseNow); cmd != nil {
		t.Fatal("small confirmation submitted")
	}
	if tm, _ := a.teamByID(parent); tm.Closed() {
		t.Fatal("small confirmation disbanded team")
	}
}

func TestTeamsFormerInteractionAliasSurvivesMembershipRemoval(t *testing.T) {
	tm := team{FormerMembers: []teamstore.Member{{Key: "old", Handle: "old-alias"}}}
	if got := teamsInteractionMember(tm, "old-alias"); got != "old" {
		t.Fatalf("history link lost: %s", got)
	}
}

func TestTeamsHistoricalInteractionsNeverFollowReusedAliasesOrNewManagers(t *testing.T) {
	tm := team{Manager: "new-manager", FormerManager: "old-manager", Members: []teamstore.Member{{Key: "new", Handle: "worker"}}, FormerMembers: []teamstore.Member{{Key: "old", Handle: "worker"}}}
	if teamsInteractionMember(tm, "worker") != "" || teamsInteractionMember(tm, teamstore.FromManager) != "" {
		t.Fatal("ambiguous old history guessed a conversation")
	}
	old := teamstore.Entry{From: "worker", FromKey: "old", To: teamstore.ToManager, ToKey: "old-manager"}
	if teamsInteractionIdentity(tm, old, true) != "old" || teamsInteractionIdentity(tm, old, false) != "old-manager" {
		t.Fatal("stable history identities followed replacements")
	}
}

// Home must confirm the saved conversation already held behind it, never start
// a new conversation merely to satisfy a destructive slash command.
func TestHomeDeleteSlashConfirmsHeldConversationWithoutStarting(t *testing.T) {
	a, _, _ := menuApp(t)
	a.page = pageHome
	a.start, a.fresh = nil, nil
	file, owner := a.file, a.agent
	a.homeSlash("/delete")
	if !a.cdelete.on || a.cdelete.file != file || a.file != file || a.agent != owner {
		t.Fatal("Home deletion did not confirm the existing conversation")
	}
	a.cdelete = conversationDeleteSheet{}
	a.file = ""
	a.homeSlash("/delete")
	if a.cdelete.on || a.home.msg != "This conversation has no saved transcript yet" {
		t.Fatalf("unsaved Home deletion did not explain the refusal: %q", a.home.msg)
	}
}

func TestConversationDeleteSimpleDefaultCancelAndTaskDoesNotReplaceManager(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 100, 30
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == parent {
			a.wall.teams[i].Manager = a.frontTabKey()
		}
	}
	a.conversationDeleteOpen(a.file, "manager")
	text := ansi.Strip(a.conversationDeleteOver(strings.Repeat("\n", 30)))
	if a.cdelete.cursor != 0 || len(a.conversationDeleteOptions()) != 2 {
		t.Fatal("confirmation does not default to cancel")
	}
	for _, word := range []string{"Memberships end", "Details", "Delete manager"} {
		if strings.Contains(text, word) {
			t.Fatalf("simple dialog contains %q", word)
		}
	}
	if !strings.Contains(text, "enter choose · esc cancel") {
		t.Fatal("footer hints missing")
	}
	a.conversationDeleteChoose(1)
	if !a.cdelete.managing || a.cdelete.cursor != 0 {
		t.Fatal("manager step did not follow yes")
	}
	called := false
	a.deleteTask = func(string, string) error { called = true; return nil }
	a.taskDeleteOpen(session.SessionRow{Transcript: a.file}, session.TaskIndexEntry{ID: "1", Title: "a task"})
	drain(t, a, a.conversationDeleteChoose(1))
	if !called {
		t.Fatal("task deletion demanded manager replacement")
	}
}

func TestTaskDeleteUsesContainmentAndIgnoresLateUpdates(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 100, 30
	owner := filepath.Base(filepath.Dir(a.file))
	a.taskSheet.reading.items = []tasksItem{
		{entry: session.TaskIndexEntry{SessionID: owner, ID: "t-parent"}},
		{entry: session.TaskIndexEntry{SessionID: owner, ID: "t-child", Parent: "t-parent"}, plan: &session.PlanTaskRow{ID: "t-child", Parent: "t-parent"}},
		{entry: session.TaskIndexEntry{SessionID: owner, ID: "t-sibling", Parent: "t-parent"}, plan: &session.PlanTaskRow{ID: "t-sibling", Parent: "t-run"}},
	}
	a.taskDeletionFinished(a.file, "t-parent")
	if !a.deletedSessionRows[tasksKey{session: owner, id: "t-child"}] || a.deletedSessionRows[tasksKey{session: owner, id: "t-sibling"}] {
		t.Fatal("display dependency grouping changed deletion scope")
	}
	a.deletedSessionRows[tasksKey{session: owner, id: "1"}] = true
	a.taskUpdate(session.Event{Task: &session.TaskNotice{ID: 1, State: session.TaskRunning}})
	if a.tasks[1] != nil {
		t.Fatal("late update restored deleted work")
	}
}

func TestTeamMemberRemovalDefaultsToCancel(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 100, 30
	tm, _ := a.teamByID(parent)
	var member string
	for _, m := range tm.Members {
		if m.Key != tm.Manager {
			member = m.Key
			break
		}
	}
	if member == "" {
		t.Fatal("fixture has no member")
	}
	a.teamMembershipOpen(parent, member)
	a.tmembers.removing = true
	text := ansi.Strip(a.teamMembershipOver(strings.Repeat("\n", 30)))
	if !strings.Contains(text, "Remove ") || a.tmembers.cursor != 0 {
		t.Fatal("removal confirmation missing or destructive default")
	}
	a.teamMembershipChoose(0)
	tm, _ = a.teamByID(parent)
	if !tm.Holds(member) {
		t.Fatal("cancel removed membership")
	}
	a.teamMembershipOpen(parent, member)
	a.tmembers.removing = true
	drain(t, a, a.teamMembershipChoose(1))
	tm, _ = a.teamByID(parent)
	if tm.Holds(member) {
		t.Fatal("yes did not remove membership")
	}
}

func TestConversationDeleteNewManagerCannotFallThroughWhileOpening(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 100, 30
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == parent {
			a.wall.teams[i].Manager = a.frontTabKey()
		}
	}
	a.start = func(string) (Conversation, error) { return Conversation{}, nil }
	a.conversationDeleteOpen(a.file, "manager")
	a.conversationDeleteChoose(1)
	a.cdelete.choices[parent] = "replacement"
	for i, o := range a.conversationDeleteOptions() {
		if o.action == 3 {
			a.conversationOpening = true
			if cmd := a.conversationDeleteChoose(i); cmd != nil || a.cdelete.busy {
				t.Fatal("pending new manager fell through to deletion")
			}
			a.conversationOpening = false
			a.cdelete.newSaid.pending = true
			if cmd := a.conversationDeleteChoose(i); cmd != nil || a.cdelete.busy {
				t.Fatal("pending membership fell through to deletion")
			}
			return
		}
	}
	t.Fatal("new manager action absent")
}

func TestConversationDeleteCreatesAndSavesABrandNewReplacementManager(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 120, 35
	a.profileDir = t.TempDir()
	a.teamsDisk.door = localTeams(a.profileDir, &a.teamsDisk.watch)
	old := a.frontTabKey()
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetManager(parent, old) }); err != nil {
		t.Fatal(err)
	}
	drain(t, a, a.teamsWrite())
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(file, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	a.start = func(string) (Conversation, error) {
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: a.workspace}, nil
	}
	a.conversationDeleteOpen(a.file, "manager")
	a.conversationDeleteChoose(1)
	drain(t, a, a.conversationDeleteNewManager(parent))
	drain(t, a, a.teamsWrite())
	replacement := a.convKey(file)
	stored, err := teamstore.Load(a.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	tm, ok := stored.Team(parent)
	if !ok || !tm.Holds(replacement) || tm.Manager != old || a.cdelete.choices[parent] != replacement || a.cdelete.newSaid.pending {
		t.Fatal("new replacement was not saved before selection")
	}
	a.deleteConversation = func(_ string, choices map[string]string, _ map[string][]string) error {
		if choices[parent] != replacement {
			t.Fatal("new manager choice lost")
		}
		return errors.New("captured deletion")
	}
	drain(t, a, a.conversationDeleteChoose(len(a.conversationDeleteOptions())-1))
}

func TestTeamsRevisedHeaderAndSidebarPlacement(t *testing.T) {
	a, parent, _ := menuApp(t)
	tm, _ := a.teamByID(parent)
	d := &teamsDraw{a: a}
	text := ansi.Strip(a.teamsOverviewHeader(d, tm, 140, 1))
	if ansi.StringWidth(text) != 140 || strings.Contains(text, "Disband...") {
		t.Fatal("header alignment or Disband label wrong")
	}
	settings, disband, add := 0, 0, 0
	for _, target := range d.targets {
		switch target.act {
		case teamsActSettings:
			settings = target.x0
		case teamsActClose:
			disband = target.x0
		case teamsActAddSubteam:
			add = target.x0
		}
	}
	if settings <= add || disband <= settings || disband < 120 {
		t.Fatal("settings and disband are not on the right")
	}
	rows := a.teamsRailRows()
	last, newAt := -1, -1
	for i, row := range rows {
		if row.kind == railRowTeam {
			last = i
		}
		if row.kind == railRowNew {
			newAt = i
		}
	}
	if newAt != last+1 {
		t.Fatal("New team is not immediately below active teams")
	}
}
