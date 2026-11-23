package appwire

import (
	"encoding/json"
	"testing"
)

type labelsEngine struct {
	fakeEngine
	seen []string
}

func (e *labelsEngine) AgentLabels(ids []string) (AgentLabels, error) {
	e.seen = ids
	return AgentLabels{Labels: []AgentLabel{{ID: "a", Title: "Release"}}, Incomplete: true}, nil
}
func TestAgentLabelsBoundedOptionalRoute(t *testing.T) {
	e := &labelsEngine{}
	out, err := methods["agents.labels"].call(e, json.RawMessage(`{"ids":["a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := out.(*AgentLabels)
	if len(e.seen) != 1 || len(p.Labels) != 1 || p.Labels[0].Title != "Release" || !p.Incomplete {
		t.Fatalf("lost metadata: %#v", p)
	}
	if _, err := methods["agents.labels"].call(&fakeEngine{}, json.RawMessage(`{"ids":["a"]}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("unsupported: %v", err)
	}
	for _, ids := range [][]string{make([]string, 33), {"../x"}, {"a/b"}, {" "}, {"x\\y"}} {
		if err := CheckAgentLabelIDs(ids); codeOf(err) != CodeBadParams {
			t.Fatalf("invalid ids: %q %v", ids, err)
		}
	}
}
