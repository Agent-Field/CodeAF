package appwire

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

type runWireEngine struct {
	fakeEngine
	calls int
}

func (e *runWireEngine) StartRun(app, run string, params map[string]any) (runs.Run, error) {
	e.calls++
	return runs.Run{App: app, DefinitionID: run, ID: "started"}, nil
}

func TestAppRunsWireRefusesDraftBeforeStartingLiveRun(t *testing.T) {
	engine := &runWireEngine{}
	if _, err := methods["runs.start"].call(engine, json.RawMessage(`{"app":"crm","run":"follow-up","draft":"preview"}`)); err == nil {
		t.Fatal("expected a preview run to stay off the live engine")
	}
	if engine.calls != 0 {
		t.Fatal("a preview called the live engine")
	}
	result, err := methods["runs.start"].call(engine, json.RawMessage(`{"app":"crm","run":"follow-up"}`))
	if err != nil || result.(*RunStarted).ID != "started" || engine.calls != 1 {
		t.Fatalf("live start = %+v, %v, calls = %d", result, err, engine.calls)
	}
}
