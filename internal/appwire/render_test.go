package appwire

import (
	"encoding/json"
	"testing"
)

type testRenderEngine struct {
	fakeEngine
	limit  int
	failed bool
}

func (e *testRenderEngine) ListRenders(agent, cursor string, limit int) (RenderPage, error) {
	e.limit = limit
	if e.failed {
		return RenderPage{}, Errorf(CodeBusy, "not ready")
	}
	return RenderPage{}, nil
}
func (e *testRenderEngine) GetRender(agent, id string) (Render, error) {
	return Render{ID: id, State: "cancelled", Charge: "unknown", Persistence: "unavailable"}, nil
}
func (e *testRenderEngine) CancelRender(agent, id string) (RenderCancel, error) {
	return RenderCancel{Outcome: "too_late", Render: Render{ID: id, State: "running", Charge: "unknown"}}, nil
}
func TestRenderRoutesKeepRefusalsAndPersistenceFacts(t *testing.T) {
	e := &testRenderEngine{}
	out, err := methods["render.list"].call(e, json.RawMessage(`{"agent":"a","limit":100000}`))
	if err != nil || e.limit != RenderPageMax {
		t.Fatalf("list: %v %d", err, e.limit)
	}
	raw, _ := json.Marshal(out)
	if string(raw) != `{"renders":[]}` {
		t.Fatalf("empty page: %s", raw)
	}
	e.failed = true
	if _, err := methods["render.list"].call(e, json.RawMessage(`{"agent":"a"}`)); codeOf(err) != CodeBusy {
		t.Fatalf("busy became success: %v", err)
	}
	got, err := methods["render.get"].call(e, json.RawMessage(`{"agent":"a","id":"rnd_x"}`))
	if err != nil {
		t.Fatal(err)
	}
	record := got.(*RenderResult).Render
	if record.Persistence != "unavailable" || record.Charge != "unknown" || record.CostUSD != nil {
		t.Fatalf("unknown facts changed: %#v", record)
	}
	cancelled, err := methods["render.cancel"].call(e, json.RawMessage(`{"agent":"a","id":"rnd_x"}`))
	if err != nil || cancelled.(*RenderCancel).Outcome != "too_late" {
		t.Fatalf("cancel: %#v %v", cancelled, err)
	}
	for name, body := range map[string]string{"render.list": `{"agent":"a"}`, "render.get": `{"agent":"a","id":"r"}`, "render.cancel": `{"agent":"a","id":"r"}`} {
		if _, err := methods[name].call(&fakeEngine{}, json.RawMessage(body)); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("%s unsupported: %v", name, err)
		}
	}
}
