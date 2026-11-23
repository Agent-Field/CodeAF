package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── set_adapt, set_fixed, set_start and the labels list ─────────────────────

// settingsEditEngine is a rules editor with the settings marker, like appBackend.
type settingsEditEngine struct{ rulesEditEngine }

func (*settingsEditEngine) PipelineSettingsEdits() {}

func TestTheSettingsFeatureIsAnnouncedOnlyByARulesEditorThatSaysSo(t *testing.T) {
	if got := withFactoryPipelineEditFeature(Hello{}, &rulesEditEngine{}); strings.Contains(strings.Join(got.Features, ","), "settings") {
		t.Fatalf("a rules editor without the marker announced %v", got.Features)
	}
	got := withFactoryPipelineEditFeature(Hello{}, &settingsEditEngine{})
	if len(got.Features) != 5 || got.Features[4] != FeatureFactoryPipelineEditSettings || FeatureFactoryPipelineEditSettings != "factory.pipeline_edit_settings" {
		t.Fatalf("a settings editor announced %v", got.Features)
	}
}

// WITHOUT THE WORD, NO SETTINGS EDIT REACHES THE ENGINE, and the source carries
// none of the four settings.
func TestSettingsEditsNeedTheSettingsFeature(t *testing.T) {
	old := &rulesEditEngine{}
	old.source = FactoryPipelineSource{State: PipelineSourceFile, Editable: true, Text: "pipeline: {}\n", Adapt: new(string)}
	for name, edits := range map[string]string{
		"set_adapt":         `[{"op":"set_adapt","adapt":"ask"}]`,
		"set_fixed":         `[{"op":"set_fixed","name":"test","fixed":true}]`,
		"set_start":         `[{"op":"set_start","max_size":"L"}]`,
		"the labels list":   `[{"op":"set_list","list":"labels","lines":["agent-ready"]}]`,
		"beside a list set": `[{"op":"set_list","list":"rules","lines":[]},{"op":"set_start","max_cost":3}]`,
	} {
		if _, err := methods["factory.pipeline_preview"].call(old, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, edits))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(old.previews) != 0 {
		t.Fatalf("a settings edit reached an engine without the feature: %+v", old.previews)
	}
	out, err := methods["factory.pipeline_source"].call(old, json.RawMessage(`{"factory":"f","repo":"a/b"}`))
	if err != nil || out.(*FactoryPipelineSource).Adapt != nil {
		t.Fatalf("a source without the feature carried settings: %+v %v", out, err)
	}
}

func TestSettingsEditsAreForwardedAndTheirChangesAlwaysAnswered(t *testing.T) {
	eng := &settingsEditEngine{}
	eng.source = FactoryPipelineSource{State: PipelineSourceFile, Editable: true, Text: "pipeline: {}\n"}
	raw := editPreviewOf(`"pipeline: {}\n"`, `[{"op":"set_adapt","adapt":"fixed"},{"op":"set_fixed","name":"test","fixed":false},{"op":"set_start","max_size":"S","max_cost":2.5},{"op":"set_list","list":"labels","lines":["agent-ready"]}]`)
	out, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	got := eng.previews[0].Edits
	if *got[0].Adapt != "fixed" || got[1].Name != "test" || *got[1].Fixed || *got[2].MaxSize != "S" || *got[2].MaxCost != 2.5 || got[3].List != PipelineListLabels {
		t.Fatalf("%+v", got)
	}
	ops := PipelineEditOps(got)
	if *ops[0].Adapt != "fixed" || *ops[1].Fixed || *ops[2].MaxCost != 2.5 {
		t.Fatalf("%+v", ops)
	}
	// The engine said nothing changed: the answer still says so, with [].
	if s := out.(*FactoryPipelinePreview).Settings; s == nil || *s == nil || len(*s) != 0 {
		t.Fatalf("settings %+v", s)
	}
	// A source carries all four, the defaults where the engine said none.
	src, err := methods["factory.pipeline_source"].call(eng, json.RawMessage(`{"factory":"f","repo":"a/b"}`))
	if err != nil {
		t.Fatal(err)
	}
	if s := src.(*FactoryPipelineSource); s.Adapt == nil || *s.Adapt != "" || s.Labels == nil || *s.MaxSize != "M" || *s.MaxCost != 5 {
		t.Fatalf("%+v", s)
	}
	// A bad word never reaches the engine.
	for _, edits := range []string{`[{"op":"set_adapt","adapt":"sometimes"}]`, `[{"op":"set_step","name":"test","prompt":"x","adapt":"ask"}]`, `[{"op":"set_start"}]`} {
		if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, edits))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", edits, err)
		}
	}
	if len(eng.previews) != 1 {
		t.Fatalf("%d previews", len(eng.previews))
	}
}
