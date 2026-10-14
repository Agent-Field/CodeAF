package crewagent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type fakeStore struct {
	svc.Store
	def  *spec.Bundle
	apps []bridge.AppSummary
}

func (f *fakeStore) Definition(context.Context, string) (*spec.Bundle, error) { return f.def, nil }
func (f *fakeStore) Get(context.Context, string) (bridge.AppDetail, error) {
	return bridge.AppDetail{AppSummary: bridge.AppSummary{State: bridge.StateKept}}, nil
}
func (f *fakeStore) List(context.Context, svc.Caller) ([]bridge.AppSummary, error) {
	return f.apps, nil
}

type fakeRecords struct {
	svc.Records
	row    bridge.Record
	scopes []svc.Scope
}

func (f *fakeRecords) Get(_ context.Context, scope svc.Scope, _ bridge.GetRecordParams) (bridge.Record, error) {
	f.scopes = append(f.scopes, scope)
	return f.row, nil
}

type fakeAgent func(context.Context, svc.Scope, bridge.AgentSendParams) (bridge.AgentSendResult, error)

func (f fakeAgent) Send(ctx context.Context, scope svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
	return f(ctx, scope, p)
}

type fakeApprovals struct {
	svc.Approvals
	opened []bridge.Approval
	scopes []svc.Scope
}

func (f *fakeApprovals) Open(_ context.Context, scope svc.Scope, a bridge.Approval) (bridge.Approval, error) {
	f.opened = append(f.opened, a)
	f.scopes = append(f.scopes, scope)
	return a, nil
}

func fixture(t *testing.T) (*Service, *svc.Services, *fakeRecords, *fakeApprovals) {
	t.Helper()
	def := &spec.Bundle{App: &spec.App{ID: "crm", Name: "CRM", Version: 12, Agent: spec.AgentRef{Name: "CRM agent"}, Records: map[string]*spec.RecordType{"deal": {Title: "name", Plural: "Deals", Fields: map[string]*spec.Field{"name": {Type: spec.FieldText}, "next_step": {Type: spec.FieldText, Label: "Next step"}, "secret": {Type: spec.FieldText, Masked: true}}}}, Capabilities: spec.Capabilities{Records: map[string]spec.Access{"deal": spec.AccessReadWrite}, Connectors: map[string]spec.Connector{"gmail": {Can: []string{"draft"}}}}, Budget: spec.Budget{Month: 10}}, Agent: &spec.AgentDoc{Name: "CRM agent"}}
	store := &fakeStore{def: def, apps: []bridge.AppSummary{{ID: "crm", State: bridge.StateKept}}}
	records := &fakeRecords{row: bridge.Record{Type: "deal", ID: "north", Version: 1, Fields: map[string]any{"name": "Northwind", "next_step": "Chase pricing", "secret": "hidden"}}}
	approvals := &fakeApprovals{}
	all := &svc.Services{Store: store, Records: records, Approvals: approvals}
	all.Hub = realtime.New(svc.Config{}, all)
	all.Caps = caps.New(svc.Config{}, all)
	service := New(svc.Config{Root: t.TempDir()}, all)
	service.now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	return service, all, records, approvals
}

func TestAskCollectsCrewTurn(t *testing.T) {
	for _, shape := range []bool{false, true} {
		name := "records and approvals"
		if shape {
			name = "shape this"
		}
		t.Run(name, func(t *testing.T) {
			service, all, records, approvals := fixture(t)
			all.Agent = fakeAgent(func(_ context.Context, scope svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
				if scope.App != "crm" || scope.Data != svc.DataReal || scope.Caller.Member != "mona" || scope.Caller.Role != svc.RoleUser || !scope.Caller.Agent || p.Thread != "crew:pricing" {
					t.Fatalf("scope=%+v params=%+v", scope, p)
				}
				if strings.Contains(p.Text, "hidden") || strings.Contains(p.Text, "UNTRUSTED SNAPSHOT") || !strings.Contains(p.Text, "Northwind") {
					t.Fatalf("linked data=%s", p.Text)
				}
				topic := bridge.TopicAgent("crm", p.Thread)
				all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentDelta, Turn: "another", Text: "ignore"})
				if shape {
					all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentShapeThis, Turn: "turn", Text: "That changes the app", Request: "Add a field"})
				} else {
					row := records.row
					row.Version = 2
					row.Fields = map[string]any{"name": "Northwind", "next_step": "Follow-up drafted", "secret": "hidden"}
					all.Hub.Publish(bridge.TopicRecords("other", "deal"), bridge.RecordDiff{Op: "upsert", Type: "deal", ID: "other", By: "mona", Record: &row})
					all.Hub.Publish(bridge.TopicRecords("crm", "deal"), bridge.RecordDiff{Op: "upsert", Type: "deal", ID: "unrelated", By: "raj", Record: &row})
					all.Hub.Publish(bridge.TopicRecords("crm", "deal"), bridge.RecordDiff{Op: "upsert", Type: "deal", ID: "north", By: "mona", Record: &row})
					all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentDelta, Turn: "turn", Text: "Drafted."})
					all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentApproval, Turn: "turn", Approval: &bridge.Approval{ID: "a1", App: "crm", Who: "manager", State: bridge.ApprovalOpen, Title: "Approval needed: send email", Payload: map[string]any{"to": "priya@example.com"}}})
				}
				all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentDone, Turn: "turn", Cost: &bridge.Cost{Spent: .62}})
				return bridge.AgentSendResult{Thread: p.Thread, Turn: "turn"}, nil
			})
			answer, err := service.Ask(context.Background(), svc.CrewAsk{App: "crm", Crew: "pricing", CrewName: "Pricing push", Asker: svc.Caller{Member: "mona", Role: svc.RoleUser}, Text: "Add a field", Linked: []bridge.Record{{Type: "deal", ID: "north", Fields: map[string]any{"name": "UNTRUSTED SNAPSHOT"}}}})
			if err != nil {
				t.Fatal(err)
			}
			if answer.Cost != .62 {
				t.Fatalf("answer=%+v", answer)
			}
			if len(records.scopes) != 1 || records.scopes[0].Caller.Member != "mona" {
				t.Fatal(records.scopes)
			}
			spent, err := service.Spent("crm", service.now())
			if err != nil || spent != .62 {
				t.Fatalf("spent=%v err=%v", spent, err)
			}
			if shape {
				if answer.ShapeThis != "Add a field" || len(answer.Changed) != 0 {
					t.Fatal(answer)
				}
				return
			}
			if answer.Text != "Drafted." || len(answer.Changed) != 1 || answer.Changed[0].Version != 12 || answer.Changed[0].What != "next step changed" || answer.Changed[0].Label != "Northwind" {
				t.Fatal(answer)
			}
			if len(approvals.opened) != 1 || len(answer.Approvals) != 1 {
				t.Fatalf("approvals=%+v", approvals)
			}
			approval := approvals.opened[0]
			if approval.Who != "mona" || approval.Payload["crew"] != "pricing" || approval.Payload["crew_name"] != "Pricing push" || approval.Payload["to"] != "priya@example.com" {
				t.Fatal(approval)
			}
		})
	}
}

func TestBadgeAndCandidatesRespectAdderRole(t *testing.T) {
	for _, role := range []svc.Role{svc.RoleViewer, svc.RoleUser, svc.RoleEditor} {
		t.Run(string(role), func(t *testing.T) {
			service, _, _, _ := fixture(t)
			caller := svc.Caller{Member: "mona", Role: role}
			badge, err := service.Badge(context.Background(), svc.Scope{App: "crm", Caller: caller})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(badge.Can, "read deals") || badge.Role != string(role) {
				t.Fatal(badge)
			}
			if role == svc.RoleViewer && badge.Can != "can read deals" {
				t.Fatal(badge)
			}
			if role != svc.RoleViewer && (!strings.Contains(badge.Can, "draft emails") || !strings.Contains(badge.Can, "change deals")) {
				t.Fatal(badge)
			}
			candidates, err := service.Candidates(context.Background(), caller)
			if err != nil || len(candidates.Agents) != 1 || candidates.Agents[0] != badge {
				t.Fatalf("candidates=%+v err=%v", candidates, err)
			}
		})
	}
}

func TestAskFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    svc.Role
		cancel  bool
		sendErr bool
		shape   string
		code    bridge.ErrorCode
	}{
		{name: "viewer denied linked type", role: svc.RoleViewer, shape: "private", code: bridge.CodeDenied},
		{name: "send failure", role: svc.RoleUser, sendErr: true, code: bridge.CodeConflict},
		{name: "cancel waiting", role: svc.RoleUser, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, all, _, _ := fixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			all.Agent = fakeAgent(func(context.Context, svc.Scope, bridge.AgentSendParams) (bridge.AgentSendResult, error) {
				if tc.sendErr {
					return bridge.AgentSendResult{}, bridge.Errorf(bridge.CodeConflict, "busy")
				}
				if tc.cancel {
					cancel()
				}
				return bridge.AgentSendResult{Thread: "crew:pricing", Turn: "turn"}, nil
			})
			linked := []bridge.Record{}
			if tc.shape != "" {
				linked = append(linked, bridge.Record{Type: tc.shape, ID: "x"})
			}
			_, err := service.Ask(ctx, svc.CrewAsk{App: "crm", Crew: "pricing", Asker: svc.Caller{Member: "mona", Role: tc.role}, Text: "read", Linked: linked})
			if tc.cancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			var typed *bridge.Error
			if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestCrewCostIsPerTurnAndAppBudgetStopsNextAsk(t *testing.T) {
	service, all, _, _ := fixture(t)
	all.Store.(*fakeStore).def.App.Budget.Month = 1
	total := float64(0)
	sends := 0
	all.Agent = fakeAgent(func(_ context.Context, _ svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
		sends++
		total += .6
		all.Hub.Publish(bridge.TopicAgent("crm", p.Thread), bridge.AgentEvent{Kind: bridge.AgentDone, Turn: "turn", Cost: &bridge.Cost{Spent: total}})
		return bridge.AgentSendResult{Thread: p.Thread, Turn: "turn"}, nil
	})
	req := svc.CrewAsk{App: "crm", Crew: "pricing", Asker: svc.Caller{Member: "mona", Role: svc.RoleUser}, Text: "read"}
	for range 2 {
		answer, err := service.Ask(context.Background(), req)
		if err != nil || answer.Cost != .6 {
			t.Fatalf("answer=%+v err=%v", answer, err)
		}
	}
	_, err := service.Ask(context.Background(), req)
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != bridge.CodeBudget || sends != 2 {
		t.Fatalf("err=%v sends=%d", err, sends)
	}
}

func TestAskCollectsAnApprovalOpenedByARunOnce(t *testing.T) {
	service, all, _, approvals := fixture(t)
	all.Agent = fakeAgent(func(_ context.Context, _ svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
		approval := bridge.Approval{ID: "run-approval", App: "crm", Who: "mona", State: bridge.ApprovalOpen, Payload: map[string]any{"crew": "pricing", "crew_name": "Pricing push"}}
		all.Hub.Publish(bridge.TopicApprovals("crm"), approval)
		topic := bridge.TopicAgent("crm", p.Thread)
		all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentApproval, Turn: "turn", Approval: &approval})
		all.Hub.Publish(topic, bridge.AgentEvent{Kind: bridge.AgentDone, Turn: "turn"})
		return bridge.AgentSendResult{Thread: p.Thread, Turn: "turn"}, nil
	})
	answer, err := service.Ask(context.Background(), svc.CrewAsk{App: "crm", Crew: "pricing", CrewName: "Pricing push", Asker: svc.Caller{Member: "mona", Role: svc.RoleUser}, Text: "Run the follow-up"})
	if err != nil || len(answer.Approvals) != 1 || len(approvals.opened) != 0 {
		t.Fatalf("answer=%+v opened=%+v err=%v", answer, approvals.opened, err)
	}
}
