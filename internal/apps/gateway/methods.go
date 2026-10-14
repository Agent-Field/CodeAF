package gateway

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func init() {
	Register(bridge.MethodAppsList, Method{Global: true, Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		apps, err := all.Store.List(ctx, scope.Caller)
		return bridge.AppsListResult{Apps: apps}, err
	}})
	Register(bridge.MethodAppsCreate, Method{Global: true, Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		var p bridge.CreateAppParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		return all.Builder.Create(ctx, scope.Caller, p)
	}})
	Register(bridge.MethodAppsGet, Method{Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
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
	}})
	Register(bridge.MethodAppsKeep, Method{Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return all.Store.Keep(ctx, scope.App)
	}})
	Register(bridge.MethodAppsDiscard, Method{Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return bridge.Empty{}, all.Store.Discard(ctx, scope.App)
	}})
	Register(bridge.MethodAppSetTint, Method{Global: true, Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, raw json.RawMessage) (any, error) {
		var p bridge.SetTintParams
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		store, ok := all.Store.(svc.TintStore)
		if !ok {
			return nil, bridge.Errorf(bridge.CodeNotBuilt, "app.setTint is not available")
		}
		return store.SetTint(ctx, scope.Caller, p)
	}})
	Register(bridge.MethodBuilderPlanEdit, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.PlanEditParams) (bridge.PlanEditResult, error) {
		return s.Builder.EditPlan
	})})
	Register(bridge.MethodBuilderWrite, Method{Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return bridge.Empty{}, all.Builder.Write(ctx, scope)
	}})
	Register(bridge.MethodBuilderCancel, Method{Handle: func(ctx context.Context, all *svc.Services, scope svc.Scope, _ json.RawMessage) (any, error) {
		return bridge.Empty{}, all.Builder.Cancel(ctx, scope)
	}})
	Register(bridge.MethodRecordsQuery, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.QueryParams) (bridge.QueryResult, error) {
		return s.Records.Query
	})})
	Register(bridge.MethodRecordsGet, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetRecordParams) (bridge.Record, error) {
		return s.Records.Get
	})})
	Register(bridge.MethodRecordsCreate, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.CreateRecordParams) (bridge.Record, error) {
		return s.Records.Create
	})})
	Register(bridge.MethodRecordsUpdate, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
		return s.Records.Update
	})})
	Register(bridge.MethodRecordsDelete, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
		return s.Records.Delete
	})})
	Register(bridge.MethodRunsStart, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.StartRunParams) (bridge.RunState, error) {
		return s.Runs.Start
	})})
	Register(bridge.MethodRunsGet, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.GetRunParams) (bridge.RunState, error) {
		return s.Runs.Get
	})})
	Register(bridge.MethodRunsList, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListRunsParams) (bridge.ListRunsResult, error) {
		return s.Runs.List
	})})
	Register(bridge.MethodApprovalsList, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.ListApprovalsParams) (bridge.ListApprovalsResult, error) {
		return s.Approvals.List
	})})
	Register(bridge.MethodApprovalsDecide, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.DecideApprovalParams) (bridge.Approval, error) {
		return s.Approvals.Decide
	})})
	Register(bridge.MethodAgentSend, Method{Handle: with(func(s *svc.Services) func(context.Context, svc.Scope, bridge.AgentSendParams) (bridge.AgentSendResult, error) {
		return s.Agent.Send
	})})
}

// with adapts a typed service method to a handler that decodes params.
func with[P, R any](pick func(*svc.Services) func(context.Context, svc.Scope, P) (R, error)) Handler {
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
