package gateway

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// handler runs one method after the capability check.
type handler func(ctx context.Context, all *svc.Services, scope svc.Scope, params json.RawMessage) (any, error)

// method is a dispatch entry. Global methods take no app.
type method struct {
	global bool
	run    handler
}

// methods is the whole wave-1 method table.
var methods = map[string]method{
	bridge.MethodAppsList: {global: true, run: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		apps, err := all.Store.List(ctx, scope.Caller)
		return bridge.AppsListResult{Apps: apps}, err
	}},
	bridge.MethodAppsCreate: {global: true, run: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		var p bridge.CreateAppParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		return all.Builder.Create(ctx, scope.Caller, p)
	}},
	bridge.MethodAppsGet: {run: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		detail, err := all.Store.Get(ctx, scope.App)
		if err != nil {
			return nil, err
		}
		if raw := detail.Files[spec.AppFile]; raw != "" {
			var app spec.App
			if err := json.Unmarshal([]byte(raw), &app); err != nil {
				return nil, err
			}
			detail.Needs = caps.Needs(&app)
		}
		return detail, nil
	}},
	bridge.MethodAppsKeep: {run: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return all.Store.Keep(ctx, scope.App)
	}},
	bridge.MethodAppsDiscard: {run: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return bridge.Empty{}, all.Store.Discard(ctx, scope.App)
	}},
	bridge.MethodBuilderPlanEdit: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.PlanEditParams) (bridge.PlanEditResult, error) {
		return s.Builder.EditPlan
	})},
	bridge.MethodBuilderWrite: {run: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return bridge.Empty{}, all.Builder.Write(ctx, scope)
	}},
	bridge.MethodBuilderCancel: {run: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return bridge.Empty{}, all.Builder.Cancel(ctx, scope)
	}},
	bridge.MethodRecordsQuery: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.QueryParams) (bridge.QueryResult, error) {
		return s.Records.Query
	})},
	bridge.MethodRecordsGet: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetRecordParams) (bridge.Record, error) {
		return s.Records.Get
	})},
	bridge.MethodRecordsCreate: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.CreateRecordParams) (bridge.Record, error) {
		return s.Records.Create
	})},
	bridge.MethodRecordsUpdate: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
		return s.Records.Update
	})},
	bridge.MethodRecordsDelete: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
		return s.Records.Delete
	})},
	bridge.MethodRunsStart: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.StartRunParams) (bridge.RunState, error) {
		return s.Runs.Start
	})},
	bridge.MethodRunsGet: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetRunParams) (bridge.RunState, error) {
		return s.Runs.Get
	})},
	bridge.MethodRunsList: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListRunsParams) (bridge.ListRunsResult, error) {
		return s.Runs.List
	})},
	bridge.MethodApprovalsList: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListApprovalsParams) (bridge.ListApprovalsResult, error) {
		return s.Approvals.List
	})},
	bridge.MethodApprovalsDecide: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.DecideApprovalParams) (bridge.Approval, error) {
		return s.Approvals.Decide
	})},
	bridge.MethodAgentSend: {run: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AgentSendParams) (bridge.AgentSendResult, error) {
		return s.Agent.Send
	})},
}

// with adapts a typed service method to a handler that decodes params.
func with[P, R any](pick func(*svc.Services) func(context.Context, svc.Scope, P) (R, error)) handler {
	return func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		var params P
		if err := decode(raw, &params); err != nil {
			return nil, err
		}
		return pick(all)(ctx, scope, params)
	}
}

func decode(raw json.RawMessage, into any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return bridge.Errorf(bridge.CodeInvalid, "params: "+err.Error())
	}
	return nil
}

var sessionMethods = spacesMethodTable(methods)
