package appwire

import (
	"encoding/json"
	"sync"
	"testing"
)

// seenEngine is the fake that keeps the seen marks it was asked for.
type seenEngine struct {
	fakeEngine
	mu   sync.Mutex
	seen []string
}

func (s *seenEngine) MarkSeen(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, id)
	return nil
}

// AGENTS.MARK_SEEN IS ANNOUNCED AND SERVED BY AN ENGINE THAT KEEPS SEEN MARKS:
// it hands the id over and answers empty, refuses a call with no id, and an
// engine without marks neither announces it nor serves it.
func TestAgentsMarkSeenGoesToTheSeenMarker(t *testing.T) {
	engine := &seenEngine{}
	w := startWireOver(t, engine, &engine.fakeEngine)
	found := false
	for _, f := range features(w.next()) {
		found = found || f == FeatureAgentUnread
	}
	if !found {
		t.Fatal("hello does not announce agents.mark_seen")
	}
	w.send(`{"id":1,"method":"agents.mark_seen","params":{"id":"a1"}}`)
	if got := w.next(); got["error"] != nil || got["result"] == nil {
		t.Fatalf("agents.mark_seen = %v", got)
	}
	w.send(`{"id":2,"method":"agents.mark_seen","params":{}}`)
	if got := w.next(); got["error"] == nil || got["error"].(map[string]any)["code"] != CodeBadParams {
		t.Fatalf("agents.mark_seen with no id = %v", got)
	}
	engine.mu.Lock()
	seen := append([]string(nil), engine.seen...)
	engine.mu.Unlock()
	if len(seen) != 1 || seen[0] != "a1" {
		t.Fatalf("the engine was asked to mark %v", seen)
	}

	plain := startWire(t, &fakeEngine{})
	for _, f := range features(plain.next()) {
		if f == FeatureAgentUnread {
			t.Fatal("an engine without seen marks announced them")
		}
	}
	plain.send(`{"id":3,"method":"agents.mark_seen","params":{"id":"a1"}}`)
	if got := plain.next(); got["error"] == nil || got["error"].(map[string]any)["code"] != CodeUnknownMethod {
		t.Fatalf("a plain engine answered %v", got)
	}
}

// UNREAD IS ALWAYS ON THE WIRE, a boolean, false as well as true, and the
// schema says agents.mark_seen takes an id.
func TestAgentUnreadIsABooleanOnTheWireAndInTheSchema(t *testing.T) {
	for _, unread := range []bool{false, true} {
		raw, err := json.Marshal(Agent{ID: "a1", State: StateIdle, Unread: unread})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if got, ok := fields["unread"].(bool); !ok || got != unread {
			t.Fatalf("unread=%v is %v on the wire: %s", unread, fields["unread"], raw)
		}
	}
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]struct {
			Params map[string]any `json:"params"`
		} `json:"methods"`
		Defs map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if got := doc.Defs["Agent"].Properties["unread"]["type"]; got != "boolean" {
		t.Fatalf("agent.unread is %v in the schema", got)
	}
	if _, ok := doc.Methods["agents.mark_seen"]; !ok {
		t.Fatal("the schema has no agents.mark_seen")
	}
}
