package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// checksEditEngine is a structure editor with the checks marker, like appBackend.
type checksEditEngine struct{ structureEditEngine }

func (*checksEditEngine) PipelineCheckEdits() {}

const wireAddCheck = `{"op":"add_step","pipeline":"issue","name":"unit","after":"write","kind":"check","prompt":"go test ./...","until":"green"}`

func TestAKindOnAddStepNeedsTheChecksFeature(t *testing.T) {
	if got := withFactoryPipelineEditFeature(Hello{}, &structureEditEngine{}); strings.Contains(strings.Join(got.Features, ","), "checks") {
		t.Fatalf("an editor without the marker announced %v", got.Features)
	}
	if got := withFactoryPipelineEditFeature(Hello{}, &checksEditEngine{}); !strings.Contains(strings.Join(got.Features, ","), FeatureFactoryPipelineEditChecks) {
		t.Fatalf("a checks editor announced %v", got.Features)
	}
	old := &structureEditEngine{}
	if _, err := methods["factory.pipeline_preview"].call(old, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, "["+wireAddCheck+"]"))); wireCode(t, err) != CodeBadParams || len(old.previews) != 0 {
		t.Fatalf("a check reached an engine without the feature: %v", err)
	}
	eng := &checksEditEngine{}
	if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, "["+wireAddCheck+"]"))); err != nil {
		t.Fatal(err)
	}
	if e := eng.previews[0].Edits[0]; e.Kind == nil || *e.Kind != "check" || PipelineEditOps(eng.previews[0].Edits)[0].Kind == nil {
		t.Fatalf("%+v", e)
	}
	// No verb but add_step names a kind, and no kind but chat or check is taken.
	for _, edits := range []string{`[{"op":"set_step","name":"plan","kind":"check"}]`, `[{"op":"add_step","pipeline":"issue","name":"x","after":"","kind":"post","prompt":"pr"}]`} {
		if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, edits))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", edits, err)
		}
	}
	if len(eng.previews) != 1 {
		t.Fatalf("%d previews", len(eng.previews))
	}
}
