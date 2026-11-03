package main

import (
 "strings"
 "testing"
 "github.com/Agent-Field/codeaf/internal/appwire"
 "github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestAppFactoryLabelEditHello(t *testing.T) {
 wire, _ := attachStep(t, startStepFactory(t))
 for _, word := range []string{"factory.job_step_edit_label", "factory.pipeline_edit_label"} {
  if !strings.Contains(wire.hello, `"`+word+`"`) { t.Errorf("hello lacks %s", word) }
 }
}

func TestAppFactoryPipelineLabelEdit(t *testing.T) {
 stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
 wire := newEditWire(t, stub, false, "remote", "token")
 var out appwire.FactoryPipelinePreview
 wire.must("factory.pipeline_preview", map[string]any{
  "factory": wire.id, "repo": prRepo, "base_sha": prBaseSHA, "base_text": editTestFile,
  "edits": []any{map[string]any{"op":"set_step", "name":"write", "label":" Security review "}},
 }, &out)
 if !out.Changed || !strings.Contains(out.Text, "label: Security review") || len(out.Steps) != 1 || strings.Join(out.Steps[0].Fields, ",") != "label" { t.Fatalf("label preview lost change: %+v", out) }
}
