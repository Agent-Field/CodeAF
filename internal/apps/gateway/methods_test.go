package gateway

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type dispatchProbe struct {
	method string
	scope  svc.Scope
	params any
}

func (p *dispatchProbe) save(method string, scope svc.Scope, params any) {
	p.method, p.scope, p.params = method, scope, params
}

type probeStore struct {
	svc.Store
	p *dispatchProbe
}

func (s probeStore) List(_ context.Context, c svc.Caller) ([]bridge.AppSummary, error) {
	s.p.save(bridge.MethodAppsList, svc.Scope{Caller: c}, nil)
	return nil, nil
}
func (s probeStore) Get(_ context.Context, app string) (bridge.AppDetail, error) {
	s.p.save(bridge.MethodAppsGet, svc.Scope{App: app}, nil)
	return bridge.AppDetail{}, nil
}
func (s probeStore) Keep(_ context.Context, app string) (bridge.AppSummary, error) {
	s.p.save(bridge.MethodAppsKeep, svc.Scope{App: app}, nil)
	return bridge.AppSummary{}, nil
}
func (s probeStore) Discard(_ context.Context, app string) error {
	s.p.save(bridge.MethodAppsDiscard, svc.Scope{App: app}, nil)
	return nil
}

type probeRecords struct {
	svc.Records
	p *dispatchProbe
}

func (s probeRecords) Query(_ context.Context, scope svc.Scope, p bridge.QueryParams) (bridge.QueryResult, error) {
	s.p.save(bridge.MethodRecordsQuery, scope, p)
	return bridge.QueryResult{}, nil
}
func (s probeRecords) Get(_ context.Context, scope svc.Scope, p bridge.GetRecordParams) (bridge.Record, error) {
	s.p.save(bridge.MethodRecordsGet, scope, p)
	return bridge.Record{}, nil
}
func (s probeRecords) Create(_ context.Context, scope svc.Scope, p bridge.CreateRecordParams) (bridge.Record, error) {
	s.p.save(bridge.MethodRecordsCreate, scope, p)
	return bridge.Record{}, nil
}
func (s probeRecords) Update(_ context.Context, scope svc.Scope, p bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	s.p.save(bridge.MethodRecordsUpdate, scope, p)
	return bridge.UpdateRecordResult{}, nil
}
func (s probeRecords) Delete(_ context.Context, scope svc.Scope, p bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	s.p.save(bridge.MethodRecordsDelete, scope, p)
	return bridge.DeleteRecordResult{}, nil
}

type probeBuilder struct {
	svc.Builder
	p *dispatchProbe
}

func (s probeBuilder) Create(_ context.Context, c svc.Caller, p bridge.CreateAppParams) (bridge.AppSummary, error) {
	s.p.save(bridge.MethodAppsCreate, svc.Scope{Caller: c}, p)
	return bridge.AppSummary{}, nil
}
func (s probeBuilder) EditPlan(_ context.Context, scope svc.Scope, p bridge.PlanEditParams) (bridge.PlanEditResult, error) {
	s.p.save(bridge.MethodBuilderPlanEdit, scope, p)
	return bridge.PlanEditResult{}, nil
}
func (s probeBuilder) Write(_ context.Context, scope svc.Scope) error {
	s.p.save(bridge.MethodBuilderWrite, scope, nil)
	return nil
}
func (s probeBuilder) Cancel(_ context.Context, scope svc.Scope) error {
	s.p.save(bridge.MethodBuilderCancel, scope, nil)
	return nil
}

type probeRuns struct {
	svc.Runs
	p *dispatchProbe
}

func (s probeRuns) Start(_ context.Context, scope svc.Scope, p bridge.StartRunParams) (bridge.RunState, error) {
	s.p.save(bridge.MethodRunsStart, scope, p)
	return bridge.RunState{}, nil
}
func (s probeRuns) Get(_ context.Context, scope svc.Scope, p bridge.GetRunParams) (bridge.RunState, error) {
	s.p.save(bridge.MethodRunsGet, scope, p)
	return bridge.RunState{}, nil
}
func (s probeRuns) List(_ context.Context, scope svc.Scope, p bridge.ListRunsParams) (bridge.ListRunsResult, error) {
	s.p.save(bridge.MethodRunsList, scope, p)
	return bridge.ListRunsResult{}, nil
}

type probeApprovals struct {
	svc.Approvals
	p *dispatchProbe
}

func (s probeApprovals) List(_ context.Context, scope svc.Scope, p bridge.ListApprovalsParams) (bridge.ListApprovalsResult, error) {
	s.p.save(bridge.MethodApprovalsList, scope, p)
	return bridge.ListApprovalsResult{}, nil
}
func (s probeApprovals) Decide(_ context.Context, scope svc.Scope, p bridge.DecideApprovalParams) (bridge.Approval, error) {
	s.p.save(bridge.MethodApprovalsDecide, scope, p)
	return bridge.Approval{}, nil
}

type probeAgent struct {
	svc.Agent
	p *dispatchProbe
}

func (s probeAgent) Send(_ context.Context, scope svc.Scope, p bridge.AgentSendParams) (bridge.AgentSendResult, error) {
	s.p.save(bridge.MethodAgentSend, scope, p)
	return bridge.AgentSendResult{}, nil
}

func TestDispatchUsesContractServiceMethods(t *testing.T) {
	scope := ownerScope("crm")
	scope.Data = svc.DataReal
	tests := []struct {
		method string
		params any
	}{
		{bridge.MethodAppsList, nil}, {bridge.MethodAppsGet, nil}, {bridge.MethodAppsCreate, bridge.CreateAppParams{Paragraph: "a sales app"}},
		{bridge.MethodAppsKeep, nil}, {bridge.MethodAppsDiscard, nil},
		{bridge.MethodBuilderPlanEdit, bridge.PlanEditParams{Edits: []bridge.PlanEdit{{ID: "deal", Text: "deals"}}}},
		{bridge.MethodBuilderWrite, nil}, {bridge.MethodBuilderCancel, nil},
		{bridge.MethodRecordsQuery, bridge.QueryParams{Type: "deal", Query: "sort:-amount", Limit: 10}},
		{bridge.MethodRecordsGet, bridge.GetRecordParams{Type: "deal", ID: "row"}},
		{bridge.MethodRecordsCreate, bridge.CreateRecordParams{Type: "deal", Set: map[string]any{"name": "deal"}}},
		{bridge.MethodRecordsUpdate, bridge.UpdateRecordParams{Type: "deal", IDs: []string{"row"}, Set: map[string]any{"stage": "Won"}}},
		{bridge.MethodRecordsDelete, bridge.DeleteRecordParams{Type: "deal", ID: "row"}},
		{bridge.MethodRunsStart, bridge.StartRunParams{Run: "follow-up", Params: map[string]any{"name": "deal"}}},
		{bridge.MethodRunsGet, bridge.GetRunParams{RunID: "run-1"}},
		{bridge.MethodRunsList, bridge.ListRunsParams{Run: "follow-up", For: "row"}},
		{bridge.MethodApprovalsList, bridge.ListApprovalsParams{State: "open"}},
		{bridge.MethodApprovalsDecide, bridge.DecideApprovalParams{ApprovalID: "approval-1", Answer: "yes"}},
		{bridge.MethodAgentSend, bridge.AgentSendParams{Thread: "thread-1", Text: "draft a follow-up", Record: "row", Screen: "deal"}},
	}
	// Connect routes have their own stub dispatch tests in connect_test.go.
	covered := make(map[string]bool, len(tests)+len(connectMethodCases))
	for _, tt := range tests {
		covered[tt.method] = true
	}
	for _, tt := range connectMethodCases {
		covered[tt.name] = true
	}
	for name := range methods {
		if !covered[name] {
			t.Errorf("method %s has no dispatch coverage", name)
		}
	}
	if len(covered) != len(methods) {
		t.Fatalf("dispatch coverage=%d methods=%d", len(covered), len(methods))
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			probe := &dispatchProbe{}
			all := &svc.Services{Store: probeStore{p: probe}, Records: probeRecords{p: probe}, Builder: probeBuilder{p: probe}, Runs: probeRuns{p: probe}, Approvals: probeApprovals{p: probe}, Agent: probeAgent{p: probe}}
			raw, _ := json.Marshal(tt.params)
			_, err := methods[tt.method].run(context.Background(), all, scope, raw)
			if err != nil {
				t.Fatal(err)
			}
			if probe.method != tt.method || !reflect.DeepEqual(probe.params, tt.params) {
				t.Fatalf("dispatch=%+v want method=%s params=%+v", probe, tt.method, tt.params)
			}
			if methods[tt.method].global {
				if probe.scope.Caller != scope.Caller {
					t.Fatal(probe.scope)
				}
			} else if probe.scope.App != "crm" {
				t.Fatal(probe.scope)
			}
		})
	}
}
