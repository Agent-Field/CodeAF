package appapi

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type fakeMemberCrewAgent struct {
	svc.CrewAgent
	scopes []svc.Scope
	denied bool
}

func (f *fakeMemberCrewAgent) Badge(_ context.Context, scope svc.Scope) (bridge.Badge, error) {
	f.scopes = append(f.scopes, scope)
	if f.denied {
		return bridge.Badge{}, bridge.Errorf(bridge.CodeDenied, "no access")
	}
	return bridge.Badge{App: scope.App, AppName: "CRM", AgentName: "CRM agent", Can: "can read deals", Role: "viewer"}, nil
}

func TestCrewAppAgentAddListDuplicateFinish(t *testing.T) {
	f := newAgentFixture(t)
	crew := workTestCrew(t, f)
	fake := &fakeMemberCrewAgent{}
	f.server.SetAppsServices(&svc.Services{Crews: svc.CrewsServices{Agent: fake}})
	path := "/app/v1/crews/" + crew.ID
	var member appAgentMemberView
	crewTestRead(t, f.request(t, "POST", path+"/members", `{"kind":"app_agent","app":"crm"}`), 201, &member)
	if member.Kind != "app_agent" || member.App != "crm" || member.Badge.Can != "can read deals" || member.State != "idle" || member.CrewID != crew.ID || len(fake.scopes) != 1 {
		t.Fatalf("member=%+v scopes=%+v", member, fake.scopes)
	}
	beforeOpens := len(f.opens)
	crewTestRead(t, f.request(t, "POST", path+"/members", `{"kind":"app_agent","app":"crm"}`), 409, nil)
	if len(f.opens) != beforeOpens {
		t.Fatal("an app agent opened a conversation")
	}
	var view struct {
		Tree []appAgentMemberView `json:"tree"`
	}
	crewTestRead(t, f.request(t, "GET", path, ""), 200, &view)
	found := false
	for _, row := range view.Tree {
		if row.App == "crm" {
			found = true
			if row.Badge != member.Badge {
				t.Fatal(row)
			}
		}
	}
	if !found {
		t.Fatalf("tree=%+v", view.Tree)
	}
	// Native updates preserve the app agent's role and badge extension.
	if err := teams.Update(f.home, func(file *teams.File) error { return nil }); err != nil {
		t.Fatal(err)
	}
	crewTestRead(t, f.request(t, "POST", path+"/wrap-up", `{}`), 200, nil)
	nativeCrewReport(t, f, crew, "")
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 200, nil)
	file, err := teams.Snapshot(f.home)
	if err != nil {
		t.Fatal(err)
	}
	closed, _ := file.Team(crew.ID)
	if closed.Holds(member.AgentID) || len(closed.FormerMembers) == 0 {
		t.Fatalf("closed=%+v", closed)
	}
	crewTestRead(t, f.request(t, "POST", path+"/members", `{"kind":"app_agent","app":"crm"}`), 409, nil)
	crewTestRead(t, f.request(t, "POST", path+"/reopen", `{"expect":{"state":"closed"}}`), 200, nil)
	crewTestRead(t, f.request(t, "POST", path+"/members", `{"kind":"app_agent","app":"crm"}`), 201, &member)
}

func TestCrewAppAgentAddRejectsInvalidAndDenied(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		denied     bool
		status     int
	}{
		{"missing app", `{"kind":"app_agent"}`, false, 422},
		{"unknown field", `{"kind":"app_agent","app":"crm","manager":true}`, false, 422},
		{"no access", `{"kind":"app_agent","app":"crm"}`, true, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAgentFixture(t)
			crew := workTestCrew(t, f)
			f.server.SetAppsServices(&svc.Services{Crews: svc.CrewsServices{Agent: &fakeMemberCrewAgent{denied: tc.denied}}})
			crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+crew.ID+"/members", tc.body), tc.status, nil)
		})
	}
}

func TestCrewAppAgentLogIncludesApprovalCards(t *testing.T) {
	f := newAgentFixture(t)
	crew := workTestCrew(t, f)
	text := "Changed in CRM through the CRM agent\nNorthwind · next step changed · CRM v12"
	id, err := teams.AppendTrafficID(f.home, crew.ID, teams.Entry{Kind: teams.KindNote, From: "crm-agent", To: teams.ToManager, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	receipt := crewagent.LogReceipt{ID: id, Text: text, Member: "app-agent:crm", Badge: bridge.Badge{App: "crm", AppName: "CRM", AgentName: "CRM agent"}, Answer: svc.CrewAnswer{Changed: []svc.ChangedRecord{{ID: "north", Version: 12}}, Approvals: []bridge.Approval{{ID: "a1", Who: "mona", Payload: map[string]any{"crew": crew.ID}}}, Cost: .62}}
	if err := crewagent.SaveReceipt(f.home, crew.ID, receipt); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Entries []crewAppLogEntry `json:"entries"`
	}
	crewTestRead(t, f.request(t, "GET", "/app/v1/crews/"+crew.ID+"/log", ""), 200, &log)
	found := false
	for _, entry := range log.Entries {
		if entry.Kind != "app_agent" {
			continue
		}
		found = true
		if !strings.Contains(entry.Text, "Changed in CRM") || len(entry.Approvals) != 1 || entry.Approvals[0].ID != "a1" || entry.Changed[0].Version != 12 || entry.Cost != .62 {
			t.Fatalf("entry=%+v", entry)
		}
	}
	if !found {
		t.Fatalf("log=%+v", log)
	}
}
