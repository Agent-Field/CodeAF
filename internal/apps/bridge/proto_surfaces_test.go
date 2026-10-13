package bridge

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSurfaceWireKeepsRequiredFalseAndZeroValues(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		fields []string
	}{
		{"part", Part{}, []string{"doc", "part", "surface", "version", "text", "updated_at", "updated_by", "by_agent"}},
		{"summary", PartSummary{}, []string{"part", "version", "comments_open"}},
		{"lock", Lock{}, []string{"doc", "part", "holder", "agent", "since", "idle_until"}},
		{"share off", SetShareParams{}, []string{"doc", "on"}},
		{"takeover handoff", AnswerPartParams{}, []string{"doc", "part", "keep"}},
		{"write", WritePartParams{}, []string{"doc", "part", "text", "base_version", "kind", "label"}},
		{"diff", DiffPartResult{}, []string{"from_text", "to_text", "lines"}},
		{"version", PartVersion{}, []string{"doc", "part", "version", "kind", "label", "by", "by_agent", "at"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			if len(object) != len(tt.fields) {
				t.Fatalf("wire=%s", raw)
			}
			for _, field := range tt.fields {
				if _, ok := object[field]; !ok {
					t.Fatalf("missing %s: %s", field, raw)
				}
			}
		})
	}
}

func TestSurfaceCommentRangeRoundTrip(t *testing.T) {
	raw := []byte(`{"id":"c1","doc":"decks/q4","part":"section-2.md","anchor":{"range":{"from":4,"to":12}},"text":"shorter","by":"mona","for":"agent","state":"open","at":"2026-10-13T12:00:00Z"}`)
	var comment Comment
	if err := json.Unmarshal(raw, &comment); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(comment)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	_ = json.Unmarshal(raw, &want)
	_ = json.Unmarshal(encoded, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("range lost: %s", encoded)
	}
}

func TestSurfaceTopicPreservesDocumentPath(t *testing.T) {
	if topic := TopicDoc("slides", "decks/q4-board"); topic != "app/slides/doc/decks/q4-board" {
		t.Fatalf("topic=%q", topic)
	}
}
