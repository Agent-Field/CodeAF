package gateway

import (
	"context"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func registerCrews() bool {
	for name, entry := range crewsMethods {
		methods[name] = entry
	}
	return true
}

var crewsMethods = map[string]method{
	bridge.MethodAppsArchive: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.AppSummary, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.AppSummary, error) {
			return all.Crews.Lifecycle.Archive(ctx, scope)
		}
	})},
	bridge.MethodAppsRestore: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.AppSummary, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.AppSummary, error) {
			return all.Crews.Lifecycle.Restore(ctx, scope)
		}
	})},
	bridge.MethodAppsDelete: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.DeleteAppParams) (bridge.Empty, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.DeleteAppParams) (bridge.Empty, error) {
			return bridge.Empty{}, all.Crews.Lifecycle.Delete(ctx, scope, p)
		}
	})},
	bridge.MethodAppsCopy: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.CopyAppParams) (bridge.AppSummary, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.CopyAppParams) (bridge.AppSummary, error) {
			return all.Crews.Lifecycle.Copy(ctx, scope, p)
		}
	})},
	bridge.MethodAppsLifecycle: {global: true, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.LifecycleResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.LifecycleResult, error) {
			return all.Crews.Lifecycle.List(ctx, scope.Caller)
		}
	})},
	bridge.MethodTriggersList: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.ListTriggersResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.ListTriggersResult, error) {
			return all.Crews.Settings.ListTriggers(ctx, scope)
		}
	})},
	bridge.MethodTriggersPause: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.PauseTriggerParams) (bridge.TriggerInfo, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.PauseTriggerParams) (bridge.TriggerInfo, error) {
			return all.Crews.Settings.PauseTrigger(ctx, scope, p)
		}
	})},
	bridge.MethodBudgetGet: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.BudgetInfo, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.BudgetInfo, error) {
			return all.Crews.Settings.GetBudget(ctx, scope)
		}
	})},
	bridge.MethodBudgetSet: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.SetBudgetParams) (bridge.BudgetInfo, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.SetBudgetParams) (bridge.BudgetInfo, error) {
			return all.Crews.Settings.SetBudget(ctx, scope, p)
		}
	})},
	bridge.MethodLinksAdd: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.AddLinkParams) (bridge.CrewLink, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.AddLinkParams) (bridge.CrewLink, error) {
			return all.Crews.Links.Add(ctx, scope, p)
		}
	})},
	bridge.MethodLinksRemove: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.RemoveLinkParams) (bridge.Empty, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.RemoveLinkParams) (bridge.Empty, error) {
			return bridge.Empty{}, all.Crews.Links.Remove(ctx, scope, p)
		}
	})},
	bridge.MethodLinksForRecord: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.LinksForRecordParams) (bridge.ListLinksResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.LinksForRecordParams) (bridge.ListLinksResult, error) {
			return all.Crews.Links.ForRecord(ctx, scope, p)
		}
	})},
	bridge.MethodLinksForCrew: {global: true, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.LinksForCrewParams) (bridge.ListLinksResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.LinksForCrewParams) (bridge.ListLinksResult, error) {
			return all.Crews.Links.ForCrew(ctx, scope.Caller, p)
		}
	})},
	bridge.MethodCrewAgentBadge: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.Badge, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.Badge, error) {
			return all.Crews.Agent.Badge(ctx, scope)
		}
	})},
	bridge.MethodCrewAgentCandidates: {global: true, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.CrewAgentCandidatesResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.CrewAgentCandidatesResult, error) {
			return all.Crews.Agent.Candidates(ctx, scope.Caller)
		}
	})},
	bridge.MethodAppsPrebuilt: {global: true, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.Empty) (bridge.PrebuiltResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.Empty) (bridge.PrebuiltResult, error) {
			return all.Crews.Prebuilt.List(ctx, scope.Caller)
		}
	})},
	bridge.MethodCustomBundle: {global: false, run: with(func(all *svc.Services) func(context.Context, svc.Scope, bridge.CustomBundleParams) (bridge.CustomBundleResult, error) {
		return func(ctx context.Context, scope svc.Scope, p bridge.CustomBundleParams) (bridge.CustomBundleResult, error) {
			return all.Crews.Custom.Bundle(ctx, scope, p)
		}
	})},
}

// CrewsCaps passes crews calls through the existing capability door using the
// equivalent wave-1 operation until that door gains the spaces role resolver.
func CrewsCaps(base svc.Caps, all *svc.Services) svc.Caps {
	return crewsCaps{base: base, all: all}
}

type crewsCaps struct {
	base svc.Caps
	all  *svc.Services
}

func (c crewsCaps) Allow(ctx context.Context, scope svc.Scope, name string, raw json.RawMessage) error {
	entry, crews := crewsMethods[name]
	if crewsLinkTopic(scope, name, raw) {
		return c.base.Allow(ctx, scope, bridge.MethodAppsGet, raw)
	}
	if !crews {
		return c.base.Allow(ctx, scope, name, raw)
	}
	equivalent := bridge.MethodAppsGet
	if entry.global {
		equivalent = bridge.MethodAppsList
	}
	switch name {
	case bridge.MethodLinksAdd, bridge.MethodLinksRemove, bridge.MethodLinksForRecord:
		equivalent = bridge.MethodRecordsGet
	}
	if err := c.base.Allow(ctx, scope, equivalent, raw); err != nil {
		return err
	}
	if scope.Caller.Agent {
		return bridge.Errorf(bridge.CodeDenied, "the app agent cannot manage crews or app settings")
	}
	switch name {
	case bridge.MethodAppsArchive, bridge.MethodAppsRestore, bridge.MethodTriggersPause:
		if svc.RoleOf(scope.Caller) != svc.RoleEditor {
			return bridge.Errorf(bridge.CodeDenied, "Editor required")
		}
	case bridge.MethodAppsDelete, bridge.MethodBudgetSet:
		if svc.RoleOf(scope.Caller) != svc.RoleEditor {
			return bridge.Errorf(bridge.CodeDenied, "Owner or Admin required")
		}
		detail, err := c.all.Store.Get(ctx, scope.App)
		if err != nil {
			return err
		}
		// Workspace privilege cannot be inferred from an app role. Spaces supplies
		// that resolver; until then only Personal's combined roles can edit here.
		if detail.Space != "personal" {
			return bridge.Errorf(bridge.CodeDenied, "Owner or Admin required")
		}
	}
	return nil
}

func (s *session) resolveCrewsScope(scope svc.Scope, name string, global bool) (svc.Scope, error) {
	// A deleted app is deliberately hidden by Store.Get, but restore must still
	// reach Lifecycle, which checks archived versus deleted restore privileges.
	if name == bridge.MethodAppsRestore {
		if scope.App == "" {
			return scope, bridge.Errorf(bridge.CodeInvalid, "this method needs app")
		}
		return scope, nil
	}
	return s.resolveScope(scope, global)
}

func crewsLinkTopic(scope svc.Scope, name string, raw json.RawMessage) bool {
	if name != "sub" && name != "unsub" {
		return false
	}
	var p struct {
		Topic string `json:"topic"`
	}
	return scope.App != "" && json.Unmarshal(raw, &p) == nil && p.Topic == bridge.TopicLinks(scope.App)
}
