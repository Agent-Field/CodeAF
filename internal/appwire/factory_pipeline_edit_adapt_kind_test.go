package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// adaptKindEditEngine is a settings editor with the per-pipeline adapt marker.
type adaptKindEditEngine struct{ settingsEditEngine }

func (*adaptKindEditEngine) PipelineAdaptKindEdits() {}

const wireAdaptKind = `[{"op":"set_adapt","pipeline":"pr","adapt":"fixed"}]`

func TestSetAdaptForOnePipelineNeedsItsFeature(t *testing.T) {
	features := func(e Engine) string { return strings.Join(withFactoryPipelineEditFeature(Hello{}, e).Features, ",") }
	if got := features(&settingsEditEngine{}); strings.Contains(got, FeatureFactoryPipelineEditAdaptKind) {
		t.Fatalf("a settings editor without the marker announced %v", got)
	}
	if got := features(&adaptKindEditEngine{}); !strings.Contains(got, FeatureFactoryPipelineEditAdaptKind) {
		t.Fatalf("an adapt-kind editor announced %v", got)
	}
	preview := func(e Engine, edits string) error {
		_, err := methods["factory.pipeline_preview"].call(e, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, edits)))
		return err
	}
	old := &settingsEditEngine{}
	if err := preview(old, wireAdaptKind); wireCode(t, err) != CodeBadParams || len(old.previews) != 0 {
		t.Fatalf("a per-pipeline adapt reached an engine without the feature: %v", err)
	}
	// The whole-file word is the settings feature's, as before.
	if err := preview(old, `[{"op":"set_adapt","adapt":"ask"}]`); err != nil {
		t.Fatal(err)
	}
	eng := &adaptKindEditEngine{}
	if err := preview(eng, wireAdaptKind); err != nil {
		t.Fatal(err)
	}
	if op := PipelineEditOps(eng.previews[0].Edits)[0]; op.Pipeline != "pr" || *op.Adapt != "fixed" {
		t.Fatalf("%+v", op)
	}
	if err := preview(eng, `[{"op":"set_adapt","pipeline":"Docs!","adapt":"ask"}]`); wireCode(t, err) != CodeBadParams {
		t.Fatalf("%v", err)
	}
}

// THE SOURCE SAYS EACH PIPELINE'S WORD only behind the feature, always as a
// map for a file or a missing file.
func TestTheSourceCarriesAdaptKindsOnlyWithTheFeature(t *testing.T) {
	read := func(e interface {
		Engine
		setSource(FactoryPipelineSource)
	}, kinds *map[string]string) *FactoryPipelineSource {
		e.setSource(FactoryPipelineSource{State: PipelineSourceFile, Editable: true, Text: "pipeline: {}\n", AdaptKinds: kinds})
		out, err := methods["factory.pipeline_source"].call(e, json.RawMessage(`{"factory":"f","repo":"a/b"}`))
		if err != nil {
			t.Fatal(err)
		}
		return out.(*FactoryPipelineSource)
	}
	kinds := map[string]string{"pr": "fixed"}
	if got := read(&settingsEditEngine{}, &kinds); got.AdaptKinds != nil {
		t.Fatalf("%v", *got.AdaptKinds)
	}
	if got := read(&adaptKindEditEngine{}, nil); got.AdaptKinds == nil || len(*got.AdaptKinds) != 0 {
		t.Fatalf("%v", got.AdaptKinds)
	}
	if got := read(&adaptKindEditEngine{}, &kinds); got.AdaptKinds == nil || (*got.AdaptKinds)["pr"] != "fixed" {
		t.Fatalf("%v", got.AdaptKinds)
	}
}
