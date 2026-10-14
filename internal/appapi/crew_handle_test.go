package appapi

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestCrewFormationAddressesAgentsBeforeTheirFirstTurn(t *testing.T) {
	f := newAgentFixture(t)
	manager := crewTestAgent(t, f, "Release manager")
	fresh := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	crew := crewTestMake(t, f, "Release", manager, fresh)
	readMember := func() teams.Member {
		t.Helper()
		file, err := teams.Load(f.home)
		if err != nil {
			t.Fatal(err)
		}
		team, _ := file.Team(crew.ID)
		var path string
		if err := f.registry.disk(func(d *agentDisk) error {
			path = d.Records[fresh.ID].SessionFile
			return nil
		}, false); err != nil {
			t.Fatal(err)
		}
		member, ok := team.Member(path)
		if !ok || member.Handle == "" || teams.ValidHandle(member.Handle) != nil {
			t.Fatalf("fresh member is not addressable: %+v", member)
		}
		return member
	}
	before := readMember()
	if before.Word != "" || before.HandleBy != teams.HandleByIdentity {
		t.Fatalf("fresh member had a name or the wrong handle source: %+v", before)
	}
	file, _ := teams.Load(f.home)
	log, err := teams.ReadTraffic(f.home, crew.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := file.Team(crew.ID)
	lead, _ := team.Member(team.Manager)
	if len(log) == 0 || !strings.Contains(log[len(log)-1].Text, "Member @"+before.Handle) || !strings.Contains(log[len(log)-1].Text, "Member @"+lead.Handle) {
		t.Fatalf("handover did not give manager the valid handles: %+v", log)
	}
	readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+fresh.ID, `{"name":"checker"}`), 200)
	if after := readMember(); after.Handle != before.Handle || after.HandleBy != before.HandleBy {
		t.Fatalf("rename changed address: before=%+v after=%+v", before, after)
	}
}
