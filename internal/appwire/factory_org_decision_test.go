package appwire

import (
	"encoding/json"
	"testing"
)

func TestOrgDecisionBridgeKeepsQuestionAnswerAndNames(t *testing.T) {
	wire := []byte(`{"id":"d1","text":"Question Answer","state":"kept","by":"marco","by_name":"Marco","from":{"kind":"ask","question":"Frozen?","answer":"Frozen."},"changed_by":{"id":"lena","name":"Lena"},"changed_at":"2026-11-18T12:00:00Z","replaced_by":"d2"}`)
	var d OrgDecision
	if err := json.Unmarshal(wire, &d); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["by_name"] != "Marco" || got["from"] == nil || got["changed_by"] == nil || got["changed_at"] == nil || got["replaced_by"] != "d2" {
		t.Fatalf("bridge dropped decision facts: %s", out)
	}
	from := got["from"].(map[string]any)
	if from["question"] != "Frozen?" || from["answer"] != "Frozen." {
		t.Fatalf("bridge joined question and answer: %s", out)
	}
}
