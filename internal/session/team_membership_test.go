package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestTeamMembershipManagerToolsAddAnExistingConversationAndRemoveOnlyItsMembership(t *testing.T) {
	f := newTeamFixture(t, true)
	manager := teamAgent(t, f, f.manager, nil, nil)
	manager.teamBoundary()
	if !holds(manager, teamAddToolName) || !holds(manager, teamRemoveToolName) {
		t.Fatal("manager lacks membership tools")
	}
	id := teams.NewID()
	dir := filepath.Join(PlacesRoot(), "-membership-tools", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, placeTranscript)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := SaveMeta(dir, Meta{ID: id, Title: "Existing membership candidate", Workspace: t.TempDir(), Created: now, LastUserAt: now}); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"conversation": path})
	if said, failed, err := manager.teamAddTool(context.Background(), args); failed || err != nil {
		t.Fatalf("add: %s %v", said, err)
	}
	file, err := teams.Load(f.profile)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := file.Team(f.teamID)
	member, ok := target.Member(convKeyOf(t, path))
	if !ok {
		t.Fatal("existing conversation was not added")
	}
	other := teams.NewID()
	if err := teams.Update(f.profile, func(file *teams.File) error {
		file.Teams = append(file.Teams, teams.Team{ID: other, Name: "Other team", Manager: convKeyOf(t, f.parser)})
		return file.AddMember(other, member)
	}); err != nil {
		t.Fatal(err)
	}
	args, _ = json.Marshal(map[string]string{"handle": member.Handle})
	if said, failed, err := manager.teamRemoveTool(context.Background(), args); failed || err != nil {
		t.Fatalf("remove: %s %v", said, err)
	}
	file, err = teams.Load(f.profile)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := file.Team(f.teamID)
	second, _ := file.Team(other)
	if first.Holds(member.Key) || !second.Holds(member.Key) {
		t.Fatal("removal did not target one membership")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("removal touched the transcript")
	}
	if _, ok := file.Home(member.Key); ok {
		t.Fatal("removal assigned another reporting manager")
	}
}

func TestTeamMembershipToolsRefuseOrdinaryMembersAndManagersRemovingThemselves(t *testing.T) {
	f := newTeamFixture(t, true)
	web := teamAgent(t, f, f.web, nil, nil)
	web.teamBoundary()
	if holds(web, teamAddToolName) || holds(web, teamRemoveToolName) {
		t.Fatal("ordinary member received manager tools")
	}
	if _, failed, _ := web.teamRemoveTool(context.Background(), json.RawMessage(`{"handle":"parser"}`)); !failed {
		t.Fatal("ordinary member changed membership")
	}
	manager := teamAgent(t, f, f.manager, nil, nil)
	if said, failed, _ := manager.teamRemoveTool(context.Background(), json.RawMessage(`{"handle":"boss"}`)); !failed || !strings.Contains(said, "replacement") {
		t.Fatal("manager removal did not require a replacement")
	}
}

func TestTeamMembershipRemovalWithdrawsFormerDirectivesWhileKeepingOtherNotes(t *testing.T) {
	f := newTeamFixture(t, true)
	web := teamAgent(t, f, f.web, nil, nil)
	web.teamBoundary()
	other := teams.NewID()
	if err := teams.Update(f.profile, func(file *teams.File) error {
		file.Teams = append(file.Teams, teams.Team{ID: other, Name: "Links"})
		if err := file.SetManager(other, convKeyOf(t, f.parser)); err != nil {
			return err
		}
		return file.AddMember(other, teams.Member{Key: convKeyOf(t, f.web), File: f.web, Handle: "web", Word: "Web"})
	}); err != nil {
		t.Fatal(err)
	}
	web.teamBoundary()
	appendTraffic(t, f, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "former membership directive"})
	if err := teams.Update(f.profile, func(file *teams.File) error { return file.RemoveMember(f.teamID, convKeyOf(t, f.web)) }); err != nil {
		t.Fatal(err)
	}
	if got := web.teamBoundary(); strings.Contains(got, "former membership directive") {
		t.Fatal("removed team's queued directive was delivered")
	}
	file, err := teams.Load(f.profile)
	if err != nil {
		t.Fatal(err)
	}
	roles := rolesFor(file, []string{convKeyOf(t, f.web)}, teams.Defaults{})
	if len(roles) != 1 || !roles[0].shared || !strings.Contains(teamMemberRole(roles[0]), "independent") {
		t.Fatal("remaining membership did not describe independence")
	}
}

func TestTeamMembershipRejoiningCannotDeliverTheFormerMembershipsDirective(t *testing.T) {
	f := newTeamFixture(t, true)
	web := teamAgent(t, f, f.web, nil, nil)
	web.teamBoundary()
	appendTraffic(t, f, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "obsolete directive", At: time.Unix(1700000000, 0)})
	joined := time.Unix(1800000000, 0)
	if err := teams.Update(f.profile, func(file *teams.File) error {
		target, _ := file.Team(f.teamID)
		member, _ := target.Member(convKeyOf(t, f.web))
		if err := file.RemoveMember(f.teamID, member.Key); err != nil {
			return err
		}
		member.JoinedAt = joined
		return file.AddMember(f.teamID, member)
	}); err != nil {
		t.Fatal(err)
	}
	appendTraffic(t, f, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "new membership note", At: joined.Add(time.Second)})
	news := web.teamBoundary()
	if strings.Contains(news, "obsolete directive") || !strings.Contains(news, "new membership note") {
		t.Fatalf("rejoined membership delivered wrong traffic: %s", news)
	}
	file, _ := teams.Load(f.profile)
	role := rolesFor(file, []string{convKeyOf(t, f.web)}, teams.Defaults{})[0]
	if teamWakes(role, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "old", At: joined.Add(-time.Second)}) {
		t.Fatal("former directive wakes a rejoined member")
	}
}
