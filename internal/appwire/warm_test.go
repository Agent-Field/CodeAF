package appwire

import "testing"

// warmEngine is the fake with a warm open that says it was the one asked.
type warmEngine struct{ fakeEngine }

func (w *warmEngine) WarmAgent(id string) (Agent, error) {
	return Agent{ID: id, Title: "warmed", State: StateIdle}, nil
}

// A WARM OPEN GOES TO THE WARM OPENER, a plain one to OpenAgent, and an engine
// with no warm open refuses one rather than opening (and reopening) instead.
func TestAgentsOpenWarmGoesToTheWarmOpener(t *testing.T) {
	warm := &warmEngine{}
	w := startWireOver(t, warm, &warm.fakeEngine)
	w.next()
	w.send(`{"id":1,"method":"agents.open","params":{"id":"a1","warm":true}}`)
	if got := w.next(); got["result"].(map[string]any)["agent"].(map[string]any)["title"] != "warmed" {
		t.Fatalf("a warm open = %v", got)
	}
	w.send(`{"id":2,"method":"agents.open","params":{"id":"a1"}}`)
	if got := w.next(); got["result"].(map[string]any)["agent"].(map[string]any)["title"] == "warmed" {
		t.Fatalf("a plain open went to the warm opener: %v", got)
	}
	plain := startWire(t, &fakeEngine{})
	plain.next()
	plain.send(`{"id":3,"method":"agents.open","params":{"id":"a1","warm":true}}`)
	if got := plain.next(); got["error"] == nil || got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("a warm open on an engine without one = %v", got)
	}
}
