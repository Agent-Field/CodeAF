package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const settingsTestFile = editTestFile + `start:
  labels: [agent-ready]
  max_size: L
  max_cost: 9
adapt: ask
`

// THE SETTINGS ARE THE FILE'S OWN, read by this engine from the text, and a
// missing file says the shipped defaults; the feature is announced beside the
// rules.
func TestPipelineSourceCarriesTheFilesSettings(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	stub.source = editSourceReply(editSourceFor(prRepo, settingsTestFile))
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	if !strings.Contains(r.hello, appwire.FeatureFactoryPipelineEditSettings) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineEditSettings, r.hello)
	}
	got := r.mustSource(t)
	if got.Adapt == nil || *got.Adapt != "ask" || got.Labels == nil || !reflect.DeepEqual(*got.Labels, []string{"agent-ready"}) || *got.MaxSize != "L" || *got.MaxCost != 9 {
		t.Fatalf("%+v", got)
	}
	stub.source = editSourceReply(missingSource())
	got = r.mustSource(t)
	if got.Adapt == nil || *got.Adapt != "" || got.Labels == nil || len(*got.Labels) != 0 || *got.MaxSize != pipeline.DefaultMaxSize || *got.MaxCost != pipeline.DefaultMaxCost {
		t.Fatalf("missing %+v", got)
	}
}

// A SETTINGS PREVIEW SAYS WHAT CHANGED, and the text is the file the pull
// request commits.
func TestPipelinePreviewOfSettingsSaysWhatChanged(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	stub.source = editSourceReply(editSourceFor(prRepo, settingsTestFile))
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	src := r.mustSource(t)
	adapt, size := "fixed", "S"
	var got appwire.FactoryPipelinePreview
	r.must("factory.pipeline_preview", appwire.FactoryPipelinePreviewParams{Factory: r.id, Repo: prRepo, BaseSHA: src.BaseSHA, BaseText: src.Text,
		Edits: []appwire.FactoryPipelineEdit{{Op: appwire.PipelineEditSetAdapt, Adapt: &adapt}, {Op: appwire.PipelineEditSetStart, MaxSize: &size}}}, &got)
	want := []appwire.FactoryPipelineSettingChange{{Setting: "adapt", Before: "ask", After: "fixed"}, {Setting: "max_size", Before: "L", After: "S"}}
	if !got.Changed || got.Settings == nil || !reflect.DeepEqual(*got.Settings, want) {
		t.Fatalf("%+v", got)
	}
	back, err := pipeline.Parse(got.Text)
	if err != nil || back.Adapt != "fixed" || back.Start.MaxSize != "S" {
		t.Fatalf("%v %+v", err, back.Start)
	}
}
