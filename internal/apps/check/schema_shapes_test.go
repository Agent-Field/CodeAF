package check

import (
	"strings"
	"testing"
)

func TestOneWrongObjectDrawsOneFinding(t *testing.T) {
	at := "/commands/0/do/0"
	issues := []Issue{
		finding("app.json", at, "schema", "missing properties: 'open'", "h"),
		finding("app.json", at, "schema", "additionalProperties 'action', 'set', 'type' not allowed", "h"),
		finding("app.json", at, "schema", "missing properties: 'records.create'", "h"),
		finding("app.json", at, "schema", "additionalProperties 'set', 'type', 'action' not allowed", "h"),
		finding("app.json", at, "schema", "missing properties: 'records.delete', 'confirm'", "h"),
		finding("app.json", at, "schema", "additionalProperties 'action', 'id' not allowed", "h"),
		finding("app.json", "/name", "schema", "missing properties: 'name'", "keep"),
		finding("app.json", "/budget", "schema", "expected object, but got string", "keep"),
	}
	got := collapseShapes(issues)
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(got), got)
	}
	one := got[0]
	for _, want := range []string{"app.json:" + at, "3 allowed shapes", "open | records.create | records.delete + confirm", "keys no shape allows here: action"} {
		if !strings.Contains(one.Message, want) {
			t.Errorf("message lacks %q: %s", want, one.Message)
		}
	}
	if !strings.Contains(one.FixHint, `{"open": ...}`) || !strings.Contains(one.FixHint, "remove action") {
		t.Errorf("hint = %s", one.FixHint)
	}
	if got[1].FixHint != "keep" || got[2].FixHint != "keep" {
		t.Errorf("single findings were changed: %+v", got[1:])
	}
}
