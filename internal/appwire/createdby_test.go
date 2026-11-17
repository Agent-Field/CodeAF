package appwire

import (
	"strings"
	"testing"
)

// agents.create hands the engine who started the agent, trimmed, and refuses a
// word no record should carry.
func TestCreateCarriesCreatedBy(t *testing.T) {
	engine := &fakeEngine{}
	w := startWire(t, engine)
	w.next()
	w.send(`{"id":1,"method":"agents.create","params":{"workspace":"/w","created_by":" afi:a1 "}}`)
	if got := w.next(); got["error"] != nil {
		t.Fatalf("create = %v", got)
	}
	engine.mu.Lock()
	created := engine.created
	engine.mu.Unlock()
	if created.CreatedBy != "afi:a1" {
		t.Fatalf("created_by arrived as %q", created.CreatedBy)
	}
	for i, bad := range []string{`"a\nb"`, `"` + strings.Repeat("x", MaxCreatedByBytes+1) + `"`} {
		w.send(`{"id":` + string(rune('2'+i)) + `,"method":"agents.create","params":{"workspace":"/w","created_by":` + bad + `}}`)
		if failure, _ := w.next()["error"].(map[string]any); failure["code"] != CodeBadParams {
			t.Fatalf("created_by %s = %v", bad, failure)
		}
	}
}
