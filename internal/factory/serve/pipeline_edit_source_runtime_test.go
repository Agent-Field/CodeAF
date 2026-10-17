package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const editSourceRuntimeFile = `steps:
  plan:
    prompt: plan it
    when: thin
    effort: cheap
    fanout: per-file
  test:
    kind: check
    prompt: go test ./...
`

// Every 200 state says runtime_fields true, and the four words of a file are
// copied onto its steps.
func TestPipelineEditSourceSaysItRunsTheRuntimeWordsInEveryState(t *testing.T) {
	big := "rules:\n  - " + strings.Repeat("a", pipeline.EditTextMax) + "\n"
	for name, file := range map[string][]byte{
		"file": []byte(editSourceRuntimeFile), "legacy file": []byte(editSourceFile), "missing": nil,
		"invalid": []byte("pipeline: ["), "too large": []byte(big),
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.file = file
			w := sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath)
			var raw map[string]json.RawMessage
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &raw) != nil || string(raw["runtime_fields"]) != "true" {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
	g := newPRGitHub(t)
	g.file = []byte(editSourceRuntimeFile)
	out := readSource(t, sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath))
	by := map[string]api.PipelineEditStep{}
	for _, st := range out.Steps {
		by[st.Name] = st
	}
	if p := by["plan"]; !out.Editable || p.When != "thin" || p.Effort != "cheap" || p.Fanout != "per-file" || p.Kind != "" {
		t.Fatalf("%+v", out)
	}
	if s := by["test"]; s.Kind != "check" || !s.Locked {
		t.Fatalf("%+v", s)
	}
}
