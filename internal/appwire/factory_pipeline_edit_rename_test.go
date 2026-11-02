package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// renameEditEngine is a structure editor with the rename marker.
type renameEditEngine struct{ structureEditEngine }

func (*renameEditEngine) PipelineRenameEdits() {}

const wireRename = `{"op":"rename_step","name":"review","to":"deep-review"}`

func TestRenameStepNeedsTheRenameFeature(t *testing.T) {
	features := func(e Engine) string { return strings.Join(withFactoryPipelineEditFeature(Hello{}, e).Features, ",") }
	if got := features(&structureEditEngine{}); strings.Contains(got, FeatureFactoryPipelineEditRename) {
		t.Fatalf("an editor without the marker announced %v", got)
	}
	if got := features(&renameEditEngine{}); !strings.Contains(got, FeatureFactoryPipelineEditRename) {
		t.Fatalf("a rename editor announced %v", got)
	}
	call := func(e Engine, edits string) error {
		_, err := methods["factory.pipeline_preview"].call(e, json.RawMessage(editPreviewOf(`"pipeline: {}\n"`, edits)))
		return err
	}
	old := &structureEditEngine{}
	if err := call(old, "["+wireRename+"]"); wireCode(t, err) != CodeBadParams || len(old.previews) != 0 {
		t.Fatalf("a rename reached an engine without the feature: %v", err)
	}
	eng := &renameEditEngine{}
	if err := call(eng, "["+wireRename+"]"); err != nil {
		t.Fatal(err)
	}
	if op := PipelineEditOps(eng.previews[0].Edits)[0]; op.Op != "rename_step" || op.Name != "review" || op.To != "deep-review" {
		t.Fatalf("%+v", op)
	}
	for _, bad := range []string{
		`[{"op":"rename_step","name":"review","to":"Deep Review"}]`,
		`[{"op":"rename_step","name":"review"}]`,
		`[{"op":"set_step","name":"review","prompt":"x","to":"deep"}]`,
	} {
		if err := call(eng, bad); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(eng.previews) != 1 {
		t.Fatalf("%d previews", len(eng.previews))
	}
}
