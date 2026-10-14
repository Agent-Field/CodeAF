package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type fakeCrewAppAgent struct {
	svc.CrewAgent
	asked  []svc.CrewAsk
	answer svc.CrewAnswer
}

func (f *fakeCrewAppAgent) Ask(_ context.Context, req svc.CrewAsk) (svc.CrewAnswer, error) {
	f.asked = append(f.asked, req)
	return f.answer, nil
}

type fakeCrewAppLinks struct{ svc.CrewLinks }

func (*fakeCrewAppLinks) ForCrew(context.Context, svc.Caller, bridge.LinksForCrewParams) (bridge.ListLinksResult, error) {
	return bridge.ListLinksResult{Links: []bridge.CrewLink{
		{App: "crm", Type: "deal", ID: "north", State: bridge.CrewLinkOpen, Access: bridge.CrewLinkAccessOK},
		{App: "crm", Type: "deal", ID: "hidden", State: bridge.CrewLinkOpen, Access: bridge.CrewLinkNoAccess},
		{App: "slides", Type: "deck", ID: "q4", State: bridge.CrewLinkOpen, Access: bridge.CrewLinkAccessOK},
	}}, nil
}

func TestCrewAppsRoutesManagerAndMemberMessages(t *testing.T) {
	for _, tc := range []struct {
		name, who, tool, kind string
		added                 svc.Role
	}{
		{"manager directive", "manager", teamSendToolName, "directive", svc.RoleUser},
		{"manager note", "manager", teamSendToolName, "note", svc.RoleUser},
		{"member note", "member", teamPostToolName, "", svc.RoleViewer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTeamFixture(t, true)
			badge := bridge.Badge{App: "crm", AppName: "CRM", AgentName: "CRM agent", Can: "can read deals, draft emails", Role: string(tc.added)}
			if tc.added == svc.RoleViewer {
				badge.Can = "can read deals"
			}
			data := crewagent.MemberData{Kind: "app_agent", App: "crm", Badge: badge, Asker: svc.Caller{Member: "mona", Role: tc.added}}
			err := teams.Update(fixture.profile, func(file *teams.File) error {
				fields := map[string]any{"key": "app-agent:crm", "word": "CRM agent", "handle": "crm-agent", "handle_by": teams.HandleByTyped, "kind": data.Kind, "app": data.App, "badge": data.Badge, "asker": data.Asker}
				raw, err := json.Marshal(fields)
				if err != nil {
					return err
				}
				var member teams.Member
				if err := json.Unmarshal(raw, &member); err != nil {
					return err
				}
				return file.AddMember(fixture.teamID, member)
			})
			if err != nil {
				t.Fatal(err)
			}
			fake := &fakeCrewAppAgent{answer: svc.CrewAnswer{Text: "Drafts written.", Changed: []svc.ChangedRecord{{Type: "deal", ID: "north", Label: "Northwind", What: "next step changed", Version: 12}}, Approvals: []bridge.Approval{{ID: "approval", Who: "mona", Title: "Approval needed: send email"}}, ShapeThis: "Add a field", Cost: .62}}
			all := &svc.Services{Crews: svc.CrewsServices{Agent: fake, Links: &fakeCrewAppLinks{}}}
			SetCrewApps(fixture.profile, all, svc.Caller{Member: "mona", Role: svc.RoleEditor})
			t.Cleanup(func() { SetCrewApps(fixture.profile, nil, svc.Caller{}) })
			path := fixture.manager
			if tc.who == "member" {
				path = fixture.web
			}
			agent := teamAgent(t, fixture, path, nil, nil)
			agent.teamBoundary()
			args := map[string]any{"to": "crm-agent", "text": "Draft follow-ups", "team": fixture.teamID}
			if tc.kind != "" {
				args["kind"] = tc.kind
			}
			raw, _ := json.Marshal(args)
			found := false
			for _, tool := range agent.beltTools() {
				if tool.Name != tc.tool {
					continue
				}
				found = true
				reply, failed, err := tool.Execute(context.Background(), raw)
				if err != nil || failed || !strings.Contains(reply, "answered in the crew log") {
					t.Fatalf("reply=%s failed=%v err=%v", reply, failed, err)
				}
			}
			if !found || len(fake.asked) != 1 {
				t.Fatalf("found=%v asked=%+v", found, fake.asked)
			}
			req := fake.asked[0]
			if req.App != "crm" || req.Crew != fixture.teamID || req.Asker.Member != "mona" || req.Asker.Role != tc.added || req.Text != "Draft follow-ups" || len(req.Linked) != 1 || req.Linked[0].ID != "north" {
				t.Fatalf("ask=%+v", req)
			}
			entries, err := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 0)
			if err != nil || len(entries) != 2 {
				t.Fatalf("entries=%+v err=%v", entries, err)
			}
			answer := entries[1]
			if answer.From != "crm-agent" || answer.FromKey != "app-agent:crm" || answer.Answers != entries[0].ID || !strings.Contains(answer.Text, "Changed in CRM through the CRM agent") || !strings.Contains(answer.Text, "CRM v12") || !strings.Contains(answer.Text, "Approval needed: send email") || !strings.Contains(answer.Text, "Shape this") || !strings.Contains(answer.Text, "CRM budget") {
				t.Fatalf("answer=%+v", answer)
			}
			receipts, err := crewagent.Receipts(fixture.profile, fixture.teamID)
			if err != nil || len(receipts) != 1 || receipts[0].Answer.Approvals[0].ID != "approval" {
				t.Fatalf("receipts=%+v err=%v", receipts, err)
			}
		})
	}
}

func TestCrewAppsKeepsOrdinaryMemberRouting(t *testing.T) {
	fixture := newTeamFixture(t, true)
	agent := teamAgent(t, fixture, fixture.manager, nil, nil)
	agent.teamBoundary()
	for _, tool := range agent.beltTools() {
		if tool.Name != teamSendToolName {
			continue
		}
		text, failed, err := tool.Execute(context.Background(), json.RawMessage(`{"to":"web","text":"Read the file"}`))
		if err != nil || failed || !strings.Contains(text, "Sent a note") {
			t.Fatalf("text=%s failed=%v err=%v", text, failed, err)
		}
	}
}

func TestCrewAppsFinishCancelsEveryCopyAndKeepsOtherCrews(t *testing.T) {
	turns := &crewAppTurns{running: map[string]map[*crewAppTurn]struct{}{}}
	SetCrewApps("fixture", &svc.Services{}, svc.Caller{})
	crewAppsBindings.Store("fixture", crewAppsBinding{turns: turns})
	t.Cleanup(func() { SetCrewApps("fixture", nil, svc.Caller{}) })
	first, finishFirst := turns.start(context.Background(), "pricing")
	defer finishFirst()
	second, finishSecond := turns.start(context.Background(), "pricing")
	defer finishSecond()
	other, finishOther := turns.start(context.Background(), "review")
	defer finishOther()
	StopCrewAppAgents("fixture", "pricing")
	if first.Err() != context.Canceled || second.Err() != context.Canceled || other.Err() != nil {
		t.Fatalf("first=%v second=%v other=%v", first.Err(), second.Err(), other.Err())
	}
}
