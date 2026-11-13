package session

// team_dissolve, as tests: it is on the belt exactly while the team it would
// take back has never held anyone but its manager, it leaves with a reason the
// moment anybody joins, and a call it answers forgets the team and nothing else.
//
// Like the rest of the team tests, every fixture is a real teams.json and a
// real Traffic log, written through internal/teams.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// newHelpersFixture is the team the chat's `/helpers` makes: the fixture's
// manager conversation as manager and only member, and the web and parser
// transcripts beside it, in no team, to be added by a test.
func newHelpersFixture(t *testing.T) teamFixture {
	t.Helper()
	fixture := newTeamFixture(t, true)
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		off := false
		team := teams.Team{ID: fixture.teamID, Name: "harbor"}
		team.Settings.Wake = &off
		f.Teams = []teams.Team{team}
		if err := f.AddMember(fixture.teamID, teams.Member{Key: convKeyOf(t, fixture.manager), File: fixture.manager, Word: "Harbor manager", Handle: "boss"}); err != nil {
			return err
		}
		return f.SetManager(fixture.teamID, convKeyOf(t, fixture.manager))
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestADissolvableTeamsManagerIsOfferedTeamDissolveAndAUsedOneIsNot(t *testing.T) {
	fresh := newHelpersFixture(t)
	manager := teamAgent(t, fresh, fresh.manager, nil, nil)
	manager.teamBoundary()
	if !holds(manager, teamDissolveToolName) {
		t.Fatal("the manager of a team that never held anyone was not offered team_dissolve")
	}
	for _, name := range []string{teamStatusToolName, teamSendToolName, teamStartToolName, teamCloseReportToolName} {
		if !holds(manager, name) {
			t.Errorf("a dissolvable team's manager lacks %s, which every manager has", name)
		}
	}

	used := newTeamFixture(t, true)
	busy := teamAgent(t, used, used.manager, nil, nil)
	busy.teamBoundary()
	if holds(busy, teamDissolveToolName) {
		t.Fatal("the manager of a team with members was offered team_dissolve")
	}
	web := teamAgent(t, used, used.web, nil, nil)
	web.teamBoundary()
	if holds(web, teamDissolveToolName) {
		t.Fatal("a member was offered team_dissolve")
	}
}

// THE VERB LEAVES WHEN ANYONE JOINS, BY ANY ROUTE, and a call from memory is
// told why. The other manager verbs stay: the conversation still manages.
func TestTeamDissolveLeavesWhenAMemberJoinsAndSaysWhy(t *testing.T) {
	fixture := newHelpersFixture(t)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		return f.AddMember(fixture.teamID, teams.Member{Key: convKeyOf(t, fixture.web), File: fixture.web, Word: "web frontend", Handle: "web"})
	}); err != nil {
		t.Fatal(err)
	}
	manager.teamBoundary()
	if holds(manager, teamDissolveToolName) {
		t.Fatal("team_dissolve stayed after a member joined")
	}
	if !holds(manager, teamSendToolName) || !holds(manager, teamStartToolName) {
		t.Fatal("a member joining took the manager's other verbs")
	}
	result := manager.executeTool(context.Background(), nil, nil, withdrawnCall("c1", teamDissolveToolName, `{}`), "")
	if want := "team_dissolve is no longer one of your tools: " + teamRetiredDissolve + "."; result.text != want {
		t.Fatalf("a retired team_dissolve answered %q, want %q", result.text, want)
	}
}

// A CALL FORGETS THE TEAM, and only the team: the transcript is where it was,
// and at the next boundary every team verb leaves because there is no team.
func TestTeamDissolveForgetsTheTeamAndTheVerbsLeaveWithIt(t *testing.T) {
	fixture := newHelpersFixture(t)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	said, failed, err := manager.teamDissolveTool(context.Background(), json.RawMessage(`{}`))
	if failed || err != nil {
		t.Fatalf("dissolve: %s %v", said, err)
	}
	if !strings.Contains(said, `Dissolved "harbor"`) {
		t.Fatalf("dissolve said %q", said)
	}
	file, err := teams.Load(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := file.Team(fixture.teamID); ok || len(file.ClosedTeams()) != 0 {
		t.Fatal("the team is still in the file, open or as history")
	}
	if _, err := os.Stat(fixture.manager); err != nil {
		t.Fatal("dissolving touched the manager's transcript")
	}
	manager.teamBoundary()
	for _, name := range teamToolNames {
		if holds(manager, name) {
			t.Errorf("%s stayed on the belt of a conversation whose team was dissolved", name)
		}
	}
	result := manager.executeTool(context.Background(), nil, nil, withdrawnCall("c2", teamSendToolName, `{}`), "")
	if want := "team_send is no longer one of your tools: " + teamRetiredManager + "."; result.text != want {
		t.Fatalf("team_send after dissolving answered %q, want %q", result.text, want)
	}
}

// A START ALREADY IN THE TRAFFIC IS A MEMBER ON ITS WAY. A call made after the
// boundary armed the verb, with a team_start written since, is refused and
// changes nothing.
func TestTeamDissolveRefusesATeamWithTrafficAndWritesNothing(t *testing.T) {
	fixture := newHelpersFixture(t)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "docs", Text: "write the README"})
	before := teams.Stamp(fixture.profile)
	said, failed, _ := manager.teamDissolveTool(context.Background(), json.RawMessage(`{}`))
	if !failed || !strings.Contains(said, "not yours to take back") {
		t.Fatalf("dissolving a team with traffic said %q (failed %v)", said, failed)
	}
	if after := teams.Stamp(fixture.profile); before != after {
		t.Fatal("a refused dissolve wrote the teams file")
	}
}
