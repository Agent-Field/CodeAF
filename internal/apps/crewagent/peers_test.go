package crewagent

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type fakeSettings struct{ svc.Settings }

func (*fakeSettings) GetBudget(context.Context, svc.Scope) (bridge.BudgetInfo, error) {
	return bridge.BudgetInfo{Month: 1, Spent: .5, Currency: "USD"}, nil
}
func (*fakeSettings) SetBudget(context.Context, svc.Scope, bridge.SetBudgetParams) (bridge.BudgetInfo, error) {
	return bridge.BudgetInfo{Month: 2, Spent: .5, Currency: "USD"}, nil
}

func TestPreparedPeersOpenApprovalForAskerAndExposeAppBudget(t *testing.T) {
	service, all, _, approvals := fixture(t)
	all.Crews.Agent = service
	all.Crews.Settings = &fakeSettings{}
	all.Agent = fakeAgent(func(ctx context.Context, scope svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
		opened, err := all.Approvals.Open(ctx, scope, bridge.Approval{ID: "a1", Who: "manager", App: "crm", Title: "Approval needed: send email", State: bridge.ApprovalOpen, Payload: map[string]any{"to": "priya"}})
		if err != nil {
			return bridge.AgentSendResult{}, err
		}
		if opened.Who != "mona" || opened.Payload["crew"] != "pricing" || opened.Payload["crew_name"] != "Pricing push" {
			t.Fatalf("first opening=%+v", opened)
		}
		topic := bridge.TopicAgent("crm", p.Thread)
		all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentApproval, Turn: "turn", Approval: &opened})
		all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentShapeThis, Turn: "turn", Request: p.Text, Text: "That changes the app"})
		all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentDone, Turn: "turn", Cost: &bridge.Cost{Spent: .6}})
		return bridge.AgentSendResult{Thread: p.Thread, Turn: "turn"}, nil
	})
	Prepare(all)
	Prepare(all)
	answer, err := service.Ask(context.Background(), svc.CrewAsk{App: "crm", Crew: "pricing", CrewName: "Pricing push", Asker: svc.Caller{Member: "mona", Role: svc.RoleUser}, Text: "Add a field", Linked: []bridge.Record{{Type: "deal", ID: "north"}}})
	if err != nil || len(approvals.opened) != 1 || answer.ShapeThis != "Add a field" {
		t.Fatalf("answer=%+v opened=%+v err=%v", answer, approvals.opened, err)
	}
	info, err := all.Crews.Settings.GetBudget(context.Background(), svc.Scope{App: "crm"})
	if err != nil || info.Spent != 1.1 || !info.Paused {
		t.Fatalf("budget=%+v err=%v", info, err)
	}
	_, err = service.Ask(context.Background(), svc.CrewAsk{App: "crm", Crew: "another", Asker: svc.Caller{Member: "mona", Role: svc.RoleUser}, Text: "read"})
	if err == nil {
		t.Fatal("app budget did not stop another crew")
	}
	info, err = all.Crews.Settings.SetBudget(context.Background(), svc.Scope{App: "crm"}, bridge.SetBudgetParams{Month: 2})
	if err != nil || info.Spent != 1.1 || info.Paused {
		t.Fatalf("updated budget=%+v err=%v", info, err)
	}
}
