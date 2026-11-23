package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ── add_step, remove_step and move_step on the wire ─────────────────────────

const (
	wireAdd    = `{"op":"add_step","pipeline":"issue","name":"lint","after":"write","prompt":"run the linter"}`
	wireRemove = `{"op":"remove_step","pipeline":"pr","name":"review"}`
	wireMove   = `{"op":"move_step","pipeline":"issue","name":"test","after":""}`
)

// A STRUCTURAL EDIT REACHES ONLY AN ENGINE THAT SAYS IT PREVIEWS ONE. An editor
// without the marker refuses each verb as bad_params, exactly as before the
// verbs existed, and is never asked.
func TestStructuralEditsNeedTheStructureFeature(t *testing.T) {
	for _, edit := range []string{wireAdd, wireRemove, wireMove} {
		old := &recordingRuntimeOnly{}
		if _, err := methods["factory.pipeline_preview"].call(old, json.RawMessage(editGoodPreview("["+edit+"]"))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", edit, err)
		}
		// A set_budget beside it does not let it through either.
		if _, err := methods["factory.pipeline_preview"].call(old, json.RawMessage(editGoodPreview(`[{"op":"set_budget","value":3},`+edit+"]"))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", edit, err)
		}
		if old.calls != 0 {
			t.Fatalf("%s reached an engine without the feature", edit)
		}
		// The field edits it always took still reach it.
		if _, err := methods["factory.pipeline_preview"].call(old, json.RawMessage(editGoodPreview(`[{"op":"set_budget","value":3}]`))); err != nil || old.calls != 1 {
			t.Fatalf("%v after %d calls", err, old.calls)
		}
	}
}

func TestStructuralEditsAreForwardedExactly(t *testing.T) {
	eng := &pipelineEditEngine{}
	if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editGoodPreview("["+wireAdd+","+wireRemove+","+wireMove+"]"))); err != nil {
		t.Fatal(err)
	}
	got := eng.previews[0].Edits
	if len(got) != 3 || got[0].Op != "add_step" || got[0].Pipeline != "issue" || got[0].After == nil || *got[0].After != "write" || *got[0].Prompt != "run the linter" ||
		got[1].Op != "remove_step" || got[1].Pipeline != "pr" || got[1].After != nil ||
		got[2].Op != "move_step" || got[2].After == nil || *got[2].After != "" {
		t.Fatalf("%+v", got)
	}
	ops := PipelineEditOps(got)
	if ops[0].Pipeline != "issue" || ops[0].After != got[0].After || ops[0].Kind != nil || ops[2].After == nil || *ops[2].After != "" {
		t.Fatalf("%+v", ops)
	}
}

func TestStructuralEditShapeIsStrict(t *testing.T) {
	eng := &pipelineEditEngine{}
	for name, edit := range map[string]string{
		"add without after":    `{"op":"add_step","pipeline":"issue","name":"lint","prompt":"x"}`,
		"add without pipeline": `{"op":"add_step","name":"lint","after":"write","prompt":"x"}`,
		"add without prompt":   `{"op":"add_step","pipeline":"issue","name":"lint","after":"write"}`,
		"add a kind":           `{"op":"add_step","pipeline":"issue","name":"lint","after":"write","prompt":"x","kind":"check"}`,
		"add a model":          `{"op":"add_step","pipeline":"issue","name":"lint","after":"write","prompt":"x","model":"gpt"}`,
		"add a loop":           `{"op":"add_step","pipeline":"issue","name":"lint","after":"write","prompt":"x","loop":2}`,
		"add enabled":          `{"op":"add_step","pipeline":"issue","name":"lint","after":"write","prompt":"x","enabled":false}`,
		"add an approve":       `{"op":"add_step","pipeline":"issue","name":"approve","after":"write","prompt":"x"}`,
		"add a long name":      `{"op":"add_step","pipeline":"issue","name":"lint-pass-checker-x","after":"write","prompt":"x"}`,
		"after is a number":    `{"op":"add_step","pipeline":"issue","name":"lint","after":1,"prompt":"x"}`,
		"after is null":        `{"op":"move_step","pipeline":"issue","name":"test","after":null}`,
		"remove with after":    `{"op":"remove_step","pipeline":"pr","name":"review","after":"read"}`,
		"remove with a prompt": `{"op":"remove_step","pipeline":"pr","name":"review","prompt":"x"}`,
		"move with a max":      `{"op":"move_step","pipeline":"issue","name":"test","after":"plan","max":2}`,
		"move a position":      `{"op":"move_step","pipeline":"issue","name":"test","position":2}`,
		"move to another kind": `{"op":"move_step","pipeline":"issue","name":"test","after":"plan","to":"pr"}`,
		"reorder":              `{"op":"reorder","pipeline":"issue","names":["write","plan"]}`,
		"loop verb":            `{"op":"add_loop","pipeline":"issue","steps":["write","test"],"max":4}`,
		"set_step a pipeline":  `{"op":"set_step","pipeline":"issue","name":"write","prompt":"x"}`,
		"set_step an after":    `{"op":"set_step","name":"write","after":"plan","prompt":"x"}`,
		"budget a pipeline":    `{"op":"set_budget","pipeline":"issue","value":3}`,
		"pipeline upper":       `{"op":"remove_step","pipeline":"Issue","name":"review"}`,
	} {
		if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editGoodPreview("["+edit+"]"))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(eng.previews) != 0 {
		t.Fatalf("a bad request reached the engine: %d", len(eng.previews))
	}
}

// A preview of field edits alone carries no structure key, byte for byte what
// an older desktop already reads; a structural preview always carries all four.
func TestStructureIsOnTheAnswerOnlyForAStructuralPreview(t *testing.T) {
	raw, _ := json.Marshal(FactoryPipelinePreview{Steps: []FactoryPipelineStepChange{}})
	if strings.Contains(string(raw), "structure") || strings.Contains(string(raw), "order") {
		t.Fatalf("%s", raw)
	}
	raw, _ = json.Marshal(FactoryPipelinePreview{Steps: []FactoryPipelineStepChange{}, Structure: &FactoryPipelineStructure{
		Order: []FactoryPipelineOrderChange{}, Added: []string{}, Counts: []FactoryPipelineCount{{Pipeline: "issue", Steps: 6}}, Gates: []FactoryPipelineGateChange{}}})
	if !strings.Contains(string(raw), `"structure":{"order":[],"added":[],"counts":[{"pipeline":"issue","steps":6}],"gates":[]}`) {
		t.Fatalf("%s", raw)
	}
}

func TestStructuralBoundsAreThePipelinePackages(t *testing.T) {
	if PipelineEditAddStep != "add_step" || PipelineEditRemoveStep != "remove_step" || PipelineEditMoveStep != "move_step" {
		t.Fatal("the verbs are not the engine's own")
	}
}

// recordingRuntimeOnly is runtimeOnlyEditEngine with a call count.
type recordingRuntimeOnly struct {
	runtimeOnlyEditEngine
	calls int
}

func (r *recordingRuntimeOnly) FactoryPipelinePreview(_ context.Context, p FactoryPipelinePreviewParams) (FactoryPipelinePreview, error) {
	r.calls++
	return FactoryPipelinePreview{Factory: p.Factory, Repo: p.Repo, BaseSHA: p.BaseSHA, Text: p.BaseText}, nil
}

func (r *recordingRuntimeOnly) FactoryPipelineSource(_ context.Context, p FactoryPipelineSourceParams) (FactoryPipelineSource, error) {
	return FactoryPipelineSource{Factory: p.Factory, Repo: p.Repo, State: PipelineSourceMissing}, nil
}
