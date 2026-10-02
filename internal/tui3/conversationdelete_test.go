package tui3

import (
	"fmt"
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
