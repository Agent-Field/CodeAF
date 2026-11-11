package api

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestSetParsedCopiesTheFourRuntimeWords(t *testing.T) {
	var s PipelineEditSource
	s.SetParsed(pipeline.Source{Steps: []pipeline.StepView{
		{Name: "plan", Prompt: "p", When: "thin", Effort: "cheap", Fanout: "per-file", InFile: true},
		{Name: "test", Prompt: "go test ./...", Kind: "check", Locked: true, InFile: true},
		{Name: "old", Prompt: "p", InFile: true},
	}})
	a, b, c := s.Steps[0], s.Steps[1], s.Steps[2]
	if a.When != "thin" || a.Effort != "cheap" || a.Fanout != "per-file" || a.Kind != "" || b.Kind != "check" {
		t.Fatalf("%+v %+v", a, b)
	}
	raw, _ := json.Marshal(c)
	if string(raw) != `{"name":"old","prompt":"p","locked":false,"in_file":true}` {
		t.Fatalf("a step with no runtime word grew keys: %s", raw)
	}
}

func TestRuntimeFieldsIsAlwaysOnTheWire(t *testing.T) {
	for _, v := range []bool{true, false} {
		raw, _ := json.Marshal(PipelineEditSource{RuntimeFields: v})
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		want := "false"
		if v {
			want = "true"
		}
		if string(m["runtime_fields"]) != want {
			t.Fatalf("%s", raw)
		}
	}
}
