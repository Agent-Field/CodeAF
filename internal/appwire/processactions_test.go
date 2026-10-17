package appwire

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type actionEngine struct {
	sendEngine
	mu       sync.Mutex
	asked    []string
	deadline time.Duration
}

func (a *actionEngine) ProcessActions(_ context.Context, id string) (*ProcessActionsResult, error) {
	a.mu.Lock()
	a.asked = append(a.asked, "actions "+id)
	a.mu.Unlock()
	return &ProcessActionsResult{ID: id, Kind: ProcessKindJob, State: ProcessEnded}, nil
}
func (a *actionEngine) ProcessLogTarget(_ context.Context, id string) (*ProcessLogTargetResult, error) {
	a.mu.Lock()
	a.asked = append(a.asked, "log_target "+id)
	a.mu.Unlock()
	return &ProcessLogTargetResult{ID: id, Kind: ProcessKindJob, Local: true, Gone: true}, nil
}
func (a *actionEngine) StopAllProcesses(ctx context.Context, ask ProcessStopAllAsk) (*ProcessStopAllResult, error) {
	deadline, _ := ctx.Deadline()
	a.mu.Lock()
	a.asked = append(a.asked, "stop_all "+ask.Agent)
	a.deadline = time.Until(deadline)
	a.mu.Unlock()
	return &ProcessStopAllResult{Agent: ask.Agent, Scope: ProcessScopeAgent, NotStopped: []string{}, ServicesNotStopped: []string{}, Unsupported: []string{}}, nil
}

// THE THREE METHODS ARE ONE FEATURE OF THE ENGINES THAT SERVE THEM; an engine
// without them refuses each as unknown and hello does not name it.
func TestProcessActionsAreAFeatureOfTheEnginesThatServeThem(t *testing.T) {
	for _, name := range []string{"processes.actions", "processes.log_target", "processes.stop_all"} {
		if _, err := methods[name].call(&sendEngine{}, json.RawMessage(`{"agent":"a","id":"a:p1"}`)); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("%s on an engine without it: %v", name, err)
		}
	}
	if h := withProcessActionFeature(Hello{}, &sendEngine{}); len(h.Features) != 0 {
		t.Fatalf("hello names %v", h.Features)
	}
	if h := withProcessActionFeature(Hello{}, &actionEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureProcessesActions {
		t.Fatalf("hello names %v", h.Features)
	}
}

// ONE AGENT ONLY, CHECKED BEFORE THE ENGINE IS ASKED; the stop-all reaches the
// engine with the one ceiling from the moment it was asked.
func TestStopAllIsOneAgentUnderOneCeiling(t *testing.T) {
	eng := &actionEngine{}
	for _, bad := range []string{`{}`, `{"agent":"a","crew":"c"}`, `{"agent":"a","scope":"all"}`, `{"agent":"a\nb"}`} {
		if _, err := methods["processes.stop_all"].call(eng, json.RawMessage(bad)); codeOf(err) != CodeBadParams {
			t.Fatalf("stop_all %s: %v", bad, err)
		}
	}
	for _, name := range []string{"processes.actions", "processes.log_target"} {
		if _, err := methods[name].call(eng, json.RawMessage(`{}`)); codeOf(err) != CodeBadParams {
			t.Fatalf("%s with no id: %v", name, err)
		}
	}
	if len(eng.asked) != 0 {
		t.Fatalf("the engine was asked %q", eng.asked)
	}
	if _, err := methods["processes.stop_all"].call(eng, json.RawMessage(`{"agent":"a","scope":"agent"}`)); err != nil {
		t.Fatal(err)
	}
	if eng.deadline <= 0 || eng.deadline > ProcessStopAllCeiling {
		t.Fatalf("stop_all's deadline: %v", eng.deadline)
	}
	raw, err := methods["processes.stop_all"].call(eng, json.RawMessage(`{"agent":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(raw)
	var receipt map[string]any
	_ = json.Unmarshal(encoded, &receipt)
	for _, key := range []string{"eligible", "stopped", "already_ended", "not_stopped", "services_eligible", "services_stopped",
		"services_not_stopped", "partial", "unenumerated", "remaining_unknown", "unsupported", "jobs_deadline_ms", "services_deadline_ms"} {
		if _, ok := receipt[key]; !ok {
			t.Fatalf("the receipt left out %q: %s", key, encoded)
		}
	}
}
