package main

import (
	"context"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
	"github.com/Agent-Field/swe-pro-go/internal/session/observer"
)

// runtimeObserverDispatcher is observer.ts:456-486's LOW-tier JSON dispatch.
// dispatchJSON consumes its validated artifact by default, so the transient
// sink is .codeaf/observer/<observed-session>-<millis>.json on both sides.
type runtimeObserverDispatcher struct {
	runtime *runtimeAdapter
}

func (dispatcher runtimeObserverDispatcher) Dispatch(
	ctx context.Context,
	observed observer.ObservedSession,
	prompt string,
	outputPath string,
) (observer.Verdict, bool, error) {
	confidence := 0.0
	fallback := observer.Verdict{
		Intervene:  false,
		Reasoning:  "observer dispatch failed; defaulting to let_it_run (safe)",
		Reminder:   nil,
		Confidence: &confidence,
	}
	tier := baked.TierLow
	maxRetries := 1
	timeoutMS := int64(5 * 60_000)
	label := "observer"
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[observer.Verdict]{
		Agent: "observer", ParentSessionID: observed.ParentSessionID,
		Workspace: observed.Workspace, TaskPrompt: prompt, OutputPath: outputPath,
		Schema: observer.VerdictSchema{}, Fallback: &fallback,
		Tier: &tier, MaxRetries: &maxRetries, TimeoutMS: &timeoutMS, Label: &label,
	}, dispatcher.runtime.agentJSON())
	return result.Data, result.UsedFallback, err
}

func (runner *pipeline) startObserver(ctx context.Context, goal string) {
	if runner == nil || !observer.IsObserverEnabled() {
		return
	}
	dispatcher := runner.observerDispatcher
	if dispatcher == nil {
		dispatcher = runtimeObserverDispatcher{runtime: runner.runtime}
	}
	registry := observer.TryStart(ctx, &observer.Service{
		History: runner.runtime.durable, Reminders: runner.runtime.durable.sessions,
		Dispatcher: dispatcher, Now: runner.now,
	})
	if registry == nil {
		runner.note("[codeaf] observer enabled but registry failed to start\n")
		return
	}
	runner.observer = registry
	runner.runtime.reminders = runner.runtime.durable.sessions
	runner.runtime.registry.SetTaskObserver(registry, runner.now)
	registry.Track(observer.ObservedSession{
		SessionID: runner.sessionID, AgentRole: runner.entryAgent,
		TaskSummary: prefixUTF16(goal, 200),
		StartedAt:   float64(runner.now().UnixMilli()), Workspace: runner.workspace,
		ParentSessionID: runner.sessionID,
	})
	runner.note("[codeaf] observer sidecar enabled (CODEAF_OBSERVER=1)\n")
}

// observerTracker collapses the disabled case to a true nil interface.
// Assigning the raw *observer.Registry field into an observer.Tracker
// wraps a typed nil that passes != nil checks and panics on first use
// (the auditor-seams SIGSEGV on the round-5 merge head).
func (runner *pipeline) observerTracker() observer.Tracker {
	if runner == nil || runner.observer == nil {
		return nil
	}
	return runner.observer
}

func (runner *pipeline) stopObserver() {
	if runner == nil || runner.observer == nil {
		return
	}
	runner.runtime.registry.SetTaskObserver(nil, nil)
	runner.runtime.reminders = nil
	runner.observer.Stop()
	runner.observer = nil
	observer.ClearCurrent()
}

var _ observer.Dispatcher = runtimeObserverDispatcher{}
