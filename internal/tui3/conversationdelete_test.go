package tui3

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if a.cdelete.message != a.teamTree().ManagerRemovalMessage(a.convKey(a.cdelete.file)) || a.cdelete.cursor != 0 || len(a.conversationDeleteOptions()) != 2 {
		t.Fatal("manager deletion did not require a replacement in Teams")
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

func TestTaskDeleteRecountsSessionsBeforeQueryFiltering(t *testing.T) {
	a, _, _ := menuApp(t)
	row := session.SessionRow{ID: "owner"}
	a.taskSheet.reading = tasksReading{whole: 2, wholeChats: 1, wholeCost: 3, chats: []session.SessionRow{row}, items: []tasksItem{
		{row: row, entry: session.TaskIndexEntry{SessionID: "owner", ID: "1", Cost: 1}},
		{row: row, entry: session.TaskIndexEntry{SessionID: "owner", ID: "2", Cost: 2}},
	}}
	a.deletedSessionRows = map[tasksKey]bool{{session: "owner", id: "1"}: true}
	r := a.taskSheet.filtered(a)
	if r.whole != 1 || r.wholeChats != 1 || r.wholeCost != 2 {
		t.Fatalf("stale deletion counts: %d, %d, %f", r.whole, r.wholeChats, r.wholeCost)
	}
}

func TestConversationDeleteBorderTitleAndNoBusyFlash(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 100, 30
	a.wall.teams = nil
	a.conversationDeleteOpen(a.file, "plain")
	card, visible := a.deleteConfirmCard(0, "")
	if !visible || !strings.Contains(ansi.Strip(card.rows[0]), "Stop work and permanently delete?") {
		t.Fatal("question is not on the top border")
	}
	body := ansi.Strip(strings.Join(card.rows[1:], "\n"))
	if strings.Contains(body, "Stop work") || strings.Contains(body, "yes") || !strings.Contains(body, "delete") {
		t.Fatal("confirmation body is not minimal")
	}
	a.deleteConversation = func(string, map[string]string, map[string][]string) error { return errors.New("try again") }
	cmd := a.conversationDeleteChoose(1)
	frame := strings.Repeat("saved rows\n", 30)
	if !a.cdelete.busy || a.conversationDeleteOver(frame) != frame {
		t.Fatal("deletion drew a transient status dialog")
	}
	drain(t, a, cmd)
	if a.cdelete.busy || !a.cdelete.on || a.cdelete.message != "try again" || a.cdelete.cursor != 0 {
		t.Fatal("failure did not restore default-cancel confirmation")
	}
}

func TestHomeDeleteLastSavedRowThroughItsOptions(t *testing.T) {
	lab := newHomeLab(t)
	workspace := lab.workspace("project")
	file := lab.session("project", "last-conversation", "Last conversation", workspace, time.Now())
	a := lab.app(file)
	a.profileDir = t.TempDir()
	a.width, a.height = 140, 36
	a.behind = nil
	drain(t, a, a.openHome())
	a.home.point(file)
	drive(t, a, key("right"), key("x"))
	if !a.cdelete.on || a.cdelete.file != file {
		t.Fatal("last saved Home row did not open deletion")
	}
	drain(t, a, a.conversationDeleteChoose(1))
	if a.cdelete.on {
		t.Fatalf("last saved row deletion failed: %s", a.cdelete.message)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("last transcript survived: %v", err)
	}
	if a.file != "" || a.agent != nil || !a.at(pageHome) {
		t.Fatal("last conversation did not leave a usable Home")
	}
	for _, line := range a.home.lines {
		if line.kind == homeSession {
			t.Fatalf("deleted last row still shown: %+v", line.row)
		}
	}
}

func TestConversationDeleteBlockedUntilEveryActiveManagerChangedInTeams(t *testing.T) {
	a, parent, child := menuApp(t)
	a.width, a.height = 120, 35
	a.profileDir = t.TempDir()
	a.teamsDisk.door = localTeams(a.profileDir, &a.teamsDisk.watch)
	old := a.frontTabKey()
	for i := range a.wall.teams {
		if a.wall.teams[i].ID == parent || a.wall.teams[i].ID == child {
			a.wall.teams[i].Manager = old
			if !a.wall.teams[i].Holds(old) {
				a.wall.teams[i].Members = append(a.wall.teams[i].Members, teamMember{Key: old, File: a.file})
			}
		}
	}
	// Seed the older on-disk shape directly: new appointments must not create it.
	raw, err := json.Marshal(struct {
		Version int    `json:"version"`
		Teams   []team `json:"teams"`
	}{teamstore.Version, a.wall.teams})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(teamstore.Path(a.profileDir), raw, 0600); err != nil {
		t.Fatal(err)
	}
	a.teamsDisk.queue, a.teamsDisk.queueSeq = nil, nil

	called := false
	a.deleteConversation = func(_ string, choices map[string]string, affected map[string][]string) error {
		if len(choices) != 0 || len(affected) != 0 {
			t.Fatal("deletion changed leadership or disbanded teams")
		}
		called = true
		return errors.New("captured")
	}
	for _, id := range []string{parent, child} {
		a.conversationDeleteOpen(a.file, "manager")
		drain(t, a, a.conversationDeleteChoose(1))
		if called || a.cdelete.message != a.teamTree().ManagerRemovalMessage(a.convKey(a.cdelete.file)) {
			t.Fatalf("current manager was deletable: called=%v message=%q team=%s", called, a.cdelete.message, id)
		}
		text := ansi.Strip(a.conversationDeleteOver(strings.Repeat("\n", 35)))
		for _, old := range []string{"Choose managers", "delete and assign", "delete and create", "delete and disband"} {
			if strings.Contains(text, old) {
				t.Fatal("old manager deletion actions remain")
			}
		}
		a.conversationDeleteChoose(0)
		a.teamsDo(teamsTarget{act: teamsActChooseManager, id: id})
		rows := a.teamMembershipRows()
		if len(rows) == 0 {
			t.Fatal("no existing member offered")
		}
		drain(t, a, a.teamMembershipChoose(1))
	}
	a.conversationDeleteOpen(a.file, "former manager")
	drain(t, a, a.conversationDeleteChoose(1))
	if !called {
		t.Fatal("former manager still cannot be deleted")
	}
}

func TestTeamsChooseManagerExistingMembersOnlyAndPersists(t *testing.T) {
	a, parent, other := menuApp(t)
	a.width, a.height = 120, 35
	a.profileDir = t.TempDir()
	a.teamsDisk.door = localTeams(a.profileDir, &a.teamsDisk.watch)
	old := a.frontTabKey()
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetManager(parent, old) }); err != nil {
		t.Fatal(err)
	}
	drain(t, a, a.teamsWrite())
	a.start = func(string) (Conversation, error) {
		t.Fatal("choosing a manager created a conversation")
		return Conversation{}, nil
	}
	a.teamsDo(teamsTarget{act: teamsActChooseManager, id: parent})
	rows := a.teamMembershipRows()
	tm, _ := a.teamByID(parent)
	unrelated, _ := a.teamByID(other)
	if !a.tmembers.choosingManager || len(rows) != len(tm.Members)-1 {
		t.Fatal("wrong candidate set")
	}
	for _, row := range rows {
		if row.key == old || !tm.Holds(row.key) || unrelated.Holds(row.key) {
			t.Fatal("nonmember or current manager offered")
		}
	}
	text := ansi.Strip(a.teamMembershipOver(strings.Repeat("\n", 35)))
	if !strings.Contains(text, "Choose manager for harbor") || strings.Contains(text, "+ New conversation") || !strings.Contains(text, "enter choose · esc cancel") {
		t.Fatal("manager picker includes creation or lacks hints")
	}
	a.teamMembershipChoose(0)
	if tm, _ := a.teamByID(parent); tm.Manager != old {
		t.Fatal("cancel changed manager")
	}
	a.teamChooseManagerOpen(parent)
	selected := a.teamMembershipRows()[0].key
	drain(t, a, a.teamMembershipChoose(1))
	drain(t, a, a.teamsWrite())
	stored, err := teamstore.Load(a.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	current, ok := stored.Team(parent)
	if !ok || current.Manager != selected || !current.Holds(old) || len(current.Members) != len(tm.Members) {
		t.Fatal("manager selection did not persist or changed membership")
	}
	second, _ := stored.Team(other)
	if second.Manager != unrelated.Manager {
		t.Fatal("another team's manager changed")
	}
	// Replaying the queued edit against a changed store must not add a removed candidate.
	a.teamChooseManagerOpen(parent)
	candidate := a.teamMembershipRows()[0].key
	a.teamMembershipChoose(1)
	if len(a.teamsDisk.queue) == 0 {
		t.Fatal("manager change was not queued")
	}
	edit := a.teamsDisk.queue[len(a.teamsDisk.queue)-1]
	for i := range stored.Teams {
		if stored.Teams[i].ID == parent {
			stored.Teams[i].Manager = ""
			for j, m := range stored.Teams[i].Members {
				if m.Key == candidate {
					stored.Teams[i].Members = append(stored.Teams[i].Members[:j], stored.Teams[i].Members[j+1:]...)
					break
				}
			}
		}
	}
	if err := edit(stored); err == nil {
		t.Fatal("concurrent removal resurrected manager membership")
	}
}

func TestTeamsChooseManagerHiddenPickerCannotAct(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 120, 35
	a.teamChooseManagerOpen(parent)
	a.teamMembershipOver(strings.Repeat("\n", 35))
	if len(a.tmembers.hits) == 0 {
		t.Fatal("no picker targets")
	}
	before := mustTeam(t, a, parent).Manager
	for _, size := range [][2]int{{18, 35}, {120, 9}} {
		a.width, a.height = size[0], size[1]
		a.teamMembershipKey(key("down"))
		if cmd := a.teamMembershipKey(key("enter")); cmd != nil || mustTeam(t, a, parent).Manager != before {
			t.Fatal("hidden picker changed manager")
		}
		frame := strings.Repeat("\n", size[1])
		if a.teamMembershipOver(frame) != frame || len(a.tmembers.hits) != 0 {
			t.Fatal("hidden picker kept stale mouse targets")
		}
	}
}

func TestTeamsChooseManagerRootAndLongListsRemainReachable(t *testing.T) {
	a, parent, _ := teamsHostedLab(t)
	tm := mustTeam(t, a, parent)
	for i := 0; i < 35; i++ {
		if err := a.teamAdd(parent, []chatTab{{key: fmt.Sprintf("candidate-%d", i), file: fmt.Sprintf("/tmp/candidate-%d/transcript.jsonl", i), word: fmt.Sprintf("Candidate %d", i)}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, width := range []int{40, 72, 160} {
		a.width, a.height = width, 16
		a.teamChooseManagerOpen(parent)
		for index := range a.teamMembershipRows() {
			a.tmembers.cursor = index + 1
			text := a.teamMembershipOver(strings.Repeat("\n", 16))
			for _, line := range strings.Split(text, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatal("picker overflows terminal")
				}
			}
			found := false
			for _, hit := range a.tmembers.hits {
				found = found || hit.arg == index+1
			}
			if !found {
				t.Fatal("selected member is not visible")
			}
		}
	}
	a.teamMembershipShut()
	root := team{ID: "000000000001", Members: tm.Members}
	root.Root = true
	root.Manager = tm.Manager
	a.wall.teams = append(a.wall.teams, root)
	a.tp.sel = root.ID
	a.width, a.height = 72, 42
	if target := teamsTargetOf(t, a, teamsActChooseManager, root.ID); target.id != root.ID {
		t.Fatal("global manager cannot be replaced")
	}
}

func TestTeamsChooseManagerKeepsTheDisplayedIdentityDuringRefresh(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 120, 35
	if err := a.teamAdd(parent, []chatTab{{key: "third", file: "/tmp/third/transcript.jsonl", word: "Third"}}); err != nil {
		t.Fatal(err)
	}
	a.teamChooseManagerOpen(parent)
	a.tmembers.cursor = 1
	a.teamMembershipOver(strings.Repeat("\n", 35))
	selected := a.tmembers.shown[0].key
	before := mustTeam(t, a, parent).Manager
	if err := a.teamEdit(func(f *teamstore.File) error { return f.RemoveMember(parent, selected) }); err != nil {
		t.Fatal(err)
	}
	// Enter after the refresh but before the next frame must not select the following member.
	if cmd := a.teamMembershipChoose(1); cmd != nil || mustTeam(t, a, parent).Manager != before {
		t.Fatal("refresh substituted another manager")
	}
	a.tmembers.cursor = 1
	a.teamMembershipOver(strings.Repeat("\n", 35))
	if a.tmembers.cursor != 0 {
		t.Fatal("removed highlighted candidate did not reset to cancel")
	}
}

func TestTeamsGlobalManagerCanAddReplacementBeforeChoosing(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 120, 35
	var root string
	if err := a.teamEdit(func(f *teamstore.File) error { root = f.MakeRoot(a.now()); return f.SetManager(root, a.frontTabKey()) }); err != nil {
		t.Fatal(err)
	}
	// Only the global manager exists: no ordinary team's manager can serve as a candidate.
	a.teamMembershipOpen(root, "")
	if !a.tmembers.on {
		t.Fatal("global team has no Add member path")
	}
	added := chatTab{key: "new-global", file: "/tmp/new-global/transcript.jsonl", word: "New global manager"}
	if err := a.teamAdd(root, []chatTab{added}); err != nil {
		t.Fatal(err)
	}
	a.teamChooseManagerOpen(root)
	found := false
	for _, row := range a.teamMembershipRows() {
		found = found || row.key == added.key
	}
	if !found {
		t.Fatal("added global candidate absent from manager picker")
	}
}

func TestTeamsChooseManagerRejectsUnrelatedAndMissingCandidates(t *testing.T) {
	a, parent, other := menuApp(t)
	a.width, a.height = 120, 35
	tm := mustTeam(t, a, parent)
	candidate := tm.Members[1]
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetManager(other, candidate.Key) }); err != nil {
		t.Fatal(err)
	}
	a.teamChooseManagerOpen(parent)
	var index int
	for i, row := range a.tmembers.shown {
		if row.key == candidate.Key {
			index = i + 1
		}
	}
	before := mustTeam(t, a, parent).Manager
	a.teamMembershipChoose(index)
	if !a.tmembers.on || !strings.Contains(a.tmembers.message, "separate responsibilities") || mustTeam(t, a, parent).Manager != before {
		t.Fatal("unrelated manager appointment accepted")
	}
	a.tp.previews = map[string]teamsPreview{candidate.Key: {missing: true}}
	delete(a.behind, candidate.Key)
	a.teamChooseManagerOpen(parent)
	for _, row := range a.teamMembershipRows() {
		if row.key == candidate.Key {
			t.Fatal("missing conversation offered as replacement")
		}
	}
}

func TestConversationDeleteLongManagerRefusalStaysVisibleAndScrolls(t *testing.T) {
	a, _, _ := menuApp(t)
	a.width, a.height = 72, 16
	a.conversationDeleteOpen(a.file, "manager")
	for i := 0; i < 35; i++ {
		a.wall.teams = append(a.wall.teams, team{ID: fmt.Sprintf("legacy-%d", i), Name: fmt.Sprintf("Long team name %02d with a detailed responsibility", i), Manager: a.frontTabKey()})
	}
	a.conversationDeleteChoose(1)
	text := ansi.Strip(a.conversationDeleteOver(strings.Repeat("\n", a.height)))
	for _, word := range []string{"cancel", "delete", "This conversation manages", "scroll", "esc cancel"} {
		if !strings.Contains(text, word) {
			t.Fatalf("refusal lost %q: %s", word, text)
		}
	}
	if len(a.cdelete.hits) != 2 {
		t.Fatal("refusal hid the choices")
	}
	for i := 0; i < 100; i++ {
		a.conversationDeleteKey(key("pgdown"))
	}
	text = ansi.Strip(a.conversationDeleteOver(strings.Repeat("\n", a.height)))
	if !strings.Contains(text, "Assign another manager") || !strings.Contains(text, "deleting it") {
		t.Fatalf("last refusal lines are unreachable: %s", text)
	}
	if a.cdelete.messageTop == 0 {
		t.Fatal("refusal did not scroll")
	}
	a.height = 50
	if _, visible := a.deleteConfirmCard(0, a.cdelete.message); !visible {
		t.Fatal("resize hid refusal")
	}
}

func TestTeamsChooseManagerRechecksMissingDisplayedCandidate(t *testing.T) {
	a, parent, _ := menuApp(t)
	a.width, a.height = 120, 35
	if err := a.teamAdd(parent, []chatTab{{key: "stale", file: "/tmp/stale/transcript.jsonl", word: "Stale"}}); err != nil {
		t.Fatal(err)
	}
	a.teamChooseManagerOpen(parent)
	a.teamMembershipOver(strings.Repeat("\n", a.height))
	candidate, index := "stale", 0
	for i, row := range a.tmembers.shown {
		if row.key == candidate {
			index = i + 1
		}
	}
	if index == 0 {
		t.Fatal("candidate was not displayed")
	}
	before := mustTeam(t, a, parent).Manager
	a.tp.previews = map[string]teamsPreview{candidate: {missing: true}}
	delete(a.behind, candidate)
	a.teamMembershipChoose(index)
	if mustTeam(t, a, parent).Manager != before || !strings.Contains(a.tmembers.message, "no longer eligible") {
		t.Fatal("stale candidate became manager")
	}
}

func TestTeamsChooseManagerQueuedEditPreservesLocalAliases(t *testing.T) {
	a, _, _ := menuApp(t)
	dir := t.TempDir()
	file, alias := filepath.Join(dir, "transcript.jsonl"), filepath.Join(dir, "alias.jsonl")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, alias); err != nil {
		t.Fatal(err)
	}
	f := &teamstore.File{Teams: []team{
		{ID: "parent", Name: "Parent", Manager: alias, Members: []teamMember{{Key: alias, File: alias}}},
		{ID: "child-a", Name: "A", Parent: "parent", Manager: file, Members: []teamMember{{Key: file, File: file}}},
		{ID: "child-b", Name: "B", Parent: "parent", Manager: "other", Members: []teamMember{{Key: "other"}, {Key: file, File: file}}},
	}}
	a.profileDir = t.TempDir()
	if err := teamstore.Save(a.profileDir, f.Teams); err != nil {
		t.Fatal(err)
	}
	a.wall.teams, a.wall.loaded = f.Teams, true
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetManager("child-b", file) }); err != nil {
		t.Fatal(err)
	}
	drain(t, a, a.teamsWrite())
	stored, err := teamstore.Load(a.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	child, _ := stored.Team("child-b")
	if child.Manager != file {
		t.Fatal("valid descendant manager appointment was refused during persistence")
	}
}
