package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── set_list, the file's own lists, and a file that is not there yet ────────

// structureEditEngine is an editor from before factory.pipeline_edit_rules: it
// guards the runtime words and previews structure, and has no rules marker. It
// answers source with whatever it is given and keeps every preview.
type structureEditEngine struct {
	factoryEngine
	source   FactoryPipelineSource
	preview  FactoryPipelinePreview
	previews []FactoryPipelinePreviewParams
}

func (r *structureEditEngine) FactoryPipelineSource(_ context.Context, p FactoryPipelineSourceParams) (FactoryPipelineSource, error) {
	out := r.source
	out.Factory, out.Repo = p.Factory, p.Repo
	return out, nil
}

func (r *structureEditEngine) FactoryPipelinePreview(_ context.Context, p FactoryPipelinePreviewParams) (FactoryPipelinePreview, error) {
	r.previews = append(r.previews, p)
	out := r.preview
	out.Factory, out.Repo, out.BaseSHA = p.Factory, p.Repo, p.BaseSHA
	return out, nil
}

func (*structureEditEngine) PipelineRuntimeGuarded() {}
func (*structureEditEngine) PipelineStructureEdits() {}

// rulesEditEngine is structureEditEngine with the rules marker, like appBackend.
type rulesEditEngine struct{ structureEditEngine }

func (*rulesEditEngine) PipelineRulesEdits() {}

const wireSetList = `{"op":"set_list","list":"never","lines":["auth/","infra/**"]}`

func editPreviewOf(base, edits string) string {
	return `{"factory":"f","repo":"a/b","base_sha":"` + editSHA + `","base_text":` + base + `,"edits":` + edits + `}`
}

func TestTheRulesFeatureIsAnnouncedOnlyByAnEngineThatSaysSo(t *testing.T) {
	if got := withFactoryPipelineEditFeature(Hello{}, &structureEditEngine{}); len(got.Features) != 3 || strings.Contains(strings.Join(got.Features, ","), "rules") {
		t.Fatalf("an editor without the marker announced %v", got.Features)
	}
	got := withFactoryPipelineEditFeature(Hello{}, &rulesEditEngine{})
	if len(got.Features) != 4 || got.Features[3] != FeatureFactoryPipelineEditRules || FeatureFactoryPipelineEditRules != "factory.pipeline_edit_rules" {
		t.Fatalf("a rules editor announced %v", got.Features)
	}
	for _, other := range []string{FeatureFactoryRules, FeatureFactoryPipelinePR, FeatureFactoryPipelineEdit, FeatureFactoryPipelineEditRuntime, FeatureFactoryPipelineEditStructure} {
		if FeatureFactoryPipelineEditRules == other {
			t.Fatal("the rules feature is not distinct")
		}
	}
	// The marker alone, without an editor, announces nothing.
	if got := withFactoryPipelineEditFeature(Hello{}, &factoryEngine{}); len(got.Features) != 0 {
		t.Fatalf("%v", got.Features)
	}
}

// WITHOUT THE WORD, NOTHING NEW REACHES THE ENGINE: set_list and an empty base
// are bad_params exactly as an unknown verb and a missing base always were.
func TestSetListAndAnEmptyBaseNeedTheRulesFeature(t *testing.T) {
	old := &structureEditEngine{}
	for name, raw := range map[string]string{
		"set_list":                editPreviewOf(`"pipeline: {}\n"`, "["+wireSetList+"]"),
		"set_list beside another": editPreviewOf(`"pipeline: {}\n"`, `[{"op":"set_budget","value":3},`+wireSetList+"]"),
		"an empty base":           editPreviewOf(`""`, `[{"op":"set_budget","value":3}]`),
	} {
		if _, err := methods["factory.pipeline_preview"].call(old, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(old.previews) != 0 {
		t.Fatalf("a request for the rules feature reached an engine without it: %+v", old.previews)
	}
	if _, err := methods["factory.pipeline_preview"].call(old, json.RawMessage(editGoodPreview(`[{"op":"set_budget","value":3}]`))); err != nil || len(old.previews) != 1 {
		t.Fatalf("%v after %d calls", err, len(old.previews))
	}
}

func TestSetListAndAnEmptyBaseAreForwardedExactly(t *testing.T) {
	eng := &rulesEditEngine{}
	raw := editPreviewOf(`""`, `[{"op":"set_list","list":"rules","lines":["  keep it small  ","Ask first"]},{"op":"set_list","list":"ask_first","lines":[]}]`)
	if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	got := eng.previews[0]
	if got.BaseText != "" || len(got.Edits) != 2 {
		t.Fatalf("%+v", got)
	}
	e0, e1 := got.Edits[0], got.Edits[1]
	if e0.Op != "set_list" || e0.List != "rules" || len(e0.Lines) != 2 || e0.Lines[0] != "  keep it small  " || e0.Name != "" || e0.Prompt != nil {
		t.Fatalf("%+v", e0)
	}
	if e1.List != "ask_first" || e1.Lines == nil || len(e1.Lines) != 0 {
		t.Fatalf("an empty list is sent as one, not as none: %+v", e1)
	}
	ops := PipelineEditOps(got.Edits)
	if ops[0].List != "rules" || len(ops[0].Lines) != 2 || ops[1].Lines == nil || ops[0].Kind != nil {
		t.Fatalf("%+v", ops)
	}
}

func TestSetListShapeIsStrict(t *testing.T) {
	eng := &rulesEditEngine{}
	long := strings.Repeat("x", 301)
	var forty []string
	for n := 0; n < 41; n++ {
		forty = append(forty, `"line `+strings.Repeat("a", n+1)+`"`)
	}
	for name, edit := range map[string]string{
		"no lines":           `{"op":"set_list","list":"rules"}`,
		"null lines":         `{"op":"set_list","list":"rules","lines":null}`,
		"no list":            `{"op":"set_list","lines":["x"]}`,
		"an unknown list":    `{"op":"set_list","list":"labels","lines":["x"]}`,
		"a list upper":       `{"op":"set_list","list":"Rules","lines":["x"]}`,
		"lines a string":     `{"op":"set_list","list":"rules","lines":"x"}`,
		"a line a number":    `{"op":"set_list","list":"rules","lines":[1]}`,
		"a blank line":       `{"op":"set_list","list":"rules","lines":["a"," "]}`,
		"two lines in one":   `{"op":"set_list","list":"rules","lines":["a\nb"]}`,
		"a line too long":    `{"op":"set_list","list":"rules","lines":["` + long + `"]}`,
		"too many lines":     `{"op":"set_list","list":"rules","lines":[` + strings.Join(forty, ",") + `]}`,
		"a repeated rule":    `{"op":"set_list","list":"rules","lines":["Keep","keep"]}`,
		"a repeated path":    `{"op":"set_list","list":"never","lines":["auth/","auth/ "]}`,
		"with a name":        `{"op":"set_list","list":"rules","lines":[],"name":"write"}`,
		"with a pipeline":    `{"op":"set_list","list":"rules","lines":[],"pipeline":"issue"}`,
		"with an after":      `{"op":"set_list","list":"rules","lines":[],"after":""}`,
		"with a prompt":      `{"op":"set_list","list":"rules","lines":[],"prompt":"x"}`,
		"with a value":       `{"op":"set_list","list":"rules","lines":[],"value":3}`,
		"with a kind":        `{"op":"set_list","list":"rules","lines":[],"kind":"chat"}`,
		"a step with a list": `{"op":"set_step","name":"write","max":2,"list":"rules"}`,
		"a budget with line": `{"op":"set_budget","value":3,"lines":["x"]}`,
		"an add with a list": `{"op":"add_step","pipeline":"issue","name":"lint","after":"","prompt":"x","list":"never","lines":[]}`,
		"a line field":       `{"op":"set_list","list":"rules","line":"x"}`,
	} {
		if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editGoodPreview("["+edit+"]"))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	// base_text has to be said: omitted or null is not "no file".
	for name, raw := range map[string]string{
		"no base":    `{"factory":"f","repo":"a/b","base_sha":"` + editSHA + `","edits":[` + wireSetList + `]}`,
		"null base":  editPreviewOf(`null`, "["+wireSetList+"]"),
		"blank base": editPreviewOf(`" \n"`, "["+wireSetList+"]"),
	} {
		if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(eng.previews) != 0 {
		t.Fatalf("a bad request reached the engine: %+v", eng.previews)
	}
}

// lists is on the answer exactly when a set_list was asked for.
func TestListsAreOnTheAnswerOnlyForASetList(t *testing.T) {
	eng := &rulesEditEngine{}
	eng.preview = FactoryPipelinePreview{Lists: &[]FactoryPipelineListChange{{List: "rules", Before: []string{}, After: []string{"x"}}}}
	out, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editGoodPreview(`[{"op":"set_budget","value":3}]`)))
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(out); strings.Contains(string(raw), "lists") {
		t.Fatalf("a preview without set_list carries lists: %s", raw)
	}
	out, err = methods["factory.pipeline_preview"].call(eng, json.RawMessage(editGoodPreview("["+wireSetList+"]")))
	if raw, _ := json.Marshal(out); err != nil || !strings.Contains(string(raw), `"lists":[{"list":"rules","before":[],"after":["x"]}]`) {
		t.Fatalf("%v %s", err, raw)
	}
	eng.preview = FactoryPipelinePreview{}
	out, err = methods["factory.pipeline_preview"].call(eng, json.RawMessage(editGoodPreview("["+wireSetList+"]")))
	if raw, _ := json.Marshal(out); err != nil || !strings.Contains(string(raw), `"lists":[]`) {
		t.Fatalf("a set_list that changed nothing must still say lists: %v %s", err, raw)
	}
}

func sourceKeys(t *testing.T, e Engine) map[string]json.RawMessage {
	t.Helper()
	out, err := methods["factory.pipeline_source"].call(e, json.RawMessage(editGoodSource))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestTheSourceListsAreAbsentWithoutTheFeature(t *testing.T) {
	lines := &[]string{"keep it small"}
	for _, state := range []string{PipelineSourceFile, PipelineSourceMissing} {
		old := &structureEditEngine{source: FactoryPipelineSource{State: state, Editable: true, DefaultBudget: 5,
			Steps: []api.PipelineEditStep{{Name: "plan"}}, Rules: lines, AskFirst: lines, Never: lines}}
		keys := sourceKeys(t, old)
		for _, k := range []string{"rules", "ask_first", "never"} {
			if _, ok := keys[k]; ok {
				t.Errorf("%s: %s is on a source without the feature", state, k)
			}
		}
		// A missing file is not editable without the word, whatever the engine said.
		if state == PipelineSourceMissing && (string(keys["editable"]) != "false" || keys["steps"] != nil || keys["default_budget"] != nil) {
			t.Errorf("a missing file is editable without the feature: %v", keys)
		}
		if state == PipelineSourceFile && string(keys["editable"]) != "true" {
			t.Errorf("a file lost its editability: %v", keys)
		}
	}
}

func TestTheSourceListsAreAlwaysArraysWithTheFeature(t *testing.T) {
	for _, c := range []struct {
		state string
		has   bool
	}{{PipelineSourceFile, true}, {PipelineSourceMissing, true}, {PipelineSourceInvalid, false}, {PipelineSourceTooLarge, false}} {
		eng := &rulesEditEngine{structureEditEngine{source: FactoryPipelineSource{State: c.state, Editable: c.state == PipelineSourceFile || c.state == PipelineSourceMissing,
			Rules: &[]string{"keep it small"}}}}
		keys := sourceKeys(t, eng)
		for _, k := range []string{"rules", "ask_first", "never"} {
			got, ok := keys[k]
			if ok != c.has {
				t.Errorf("%s: %s present %v", c.state, k, ok)
			}
			if c.has && k != "rules" && string(got) != "[]" {
				t.Errorf("%s: %s = %s, want []", c.state, k, got)
			}
		}
		if c.has && string(keys["rules"]) != `["keep it small"]` {
			t.Errorf("%s: rules = %s", c.state, keys["rules"])
		}
		if c.state == PipelineSourceMissing && string(keys["editable"]) != "true" {
			t.Errorf("a missing file is not editable with the feature: %v", keys)
		}
	}
}

func TestSetListVerbAndListsAreThePipelinePackages(t *testing.T) {
	if PipelineEditSetList != "set_list" || PipelineListRules != "rules" || PipelineListAskFirst != "ask_first" || PipelineListNever != "never" {
		t.Fatal("the verb and lists are not the engine's own")
	}
}

func (r *structureEditEngine) setSource(s FactoryPipelineSource) { r.source = s }
