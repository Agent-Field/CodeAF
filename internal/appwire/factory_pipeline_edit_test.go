package appwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// pipelineEditEngine is a factory engine that is also a FactoryPipelineEditor
// and keeps what each call was handed.
type pipelineEditEngine struct {
	factoryEngine
	sources  []FactoryPipelineSourceParams
	previews []FactoryPipelinePreviewParams
	err      error
}

func (r *pipelineEditEngine) FactoryPipelineSource(_ context.Context, p FactoryPipelineSourceParams) (FactoryPipelineSource, error) {
	r.sources = append(r.sources, p)
	if r.err != nil {
		return FactoryPipelineSource{}, r.err
	}
	return FactoryPipelineSource{Factory: p.Factory, Repo: p.Repo, State: PipelineSourceMissing}, nil
}

func (r *pipelineEditEngine) FactoryPipelinePreview(_ context.Context, p FactoryPipelinePreviewParams) (FactoryPipelinePreview, error) {
	r.previews = append(r.previews, p)
	if r.err != nil {
		return FactoryPipelinePreview{}, r.err
	}
	return FactoryPipelinePreview{Factory: p.Factory, Repo: p.Repo, BaseSHA: p.BaseSHA, Text: p.BaseText}, nil
}

const (
	editSHA        = "0123456789abcdef0123456789abcdef01234567"
	editGoodSource = `{"factory":"f","repo":"a/b"}`
)

func editGoodPreview(edits string) string {
	return `{"factory":"f","repo":"a/b","base_sha":"` + editSHA + `","base_text":"pipeline: {}\n","edits":` + edits + `}`
}

// ABSENT WITHOUT THE INTERFACE, AND NOT IMPLIED BY ANY OTHER FACTORY FEATURE.
func TestFactoryPipelineEditAbsentWithoutTheInterface(t *testing.T) {
	if len(factoryPipelineEditMethods) != 2 || factoryPipelineEditMethods["factory.pipeline_source"].call == nil || factoryPipelineEditMethods["factory.pipeline_preview"].call == nil {
		t.Fatalf("methods: %v", factoryPipelineEditMethods)
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &adminEngine{}, &linkEngine{}, &repoEngine{}, &pipelinePREngine{}} {
		for name, raw := range map[string]string{"factory.pipeline_source": editGoodSource, "factory.pipeline_preview": editGoodPreview(`[{"op":"set_budget","value":3}]`)} {
			if _, err := methods[name].call(e, json.RawMessage(raw)); wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%T %s: %v", e, name, err)
			}
		}
		if got := withFactoryPipelineEditFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	// A fake or forwarding editor with the right methods but no guard marker
	// announces the edit feature and never the runtime one.
	if got := withFactoryPipelineEditFeature(Hello{}, &unguardedEditEngine{}); len(got.Features) != 1 || got.Features[0] != "factory.pipeline_edit" {
		t.Fatalf("an unguarded editor announced %v", got.Features)
	}
	got := withFactoryPipelineEditFeature(Hello{}, &pipelineEditEngine{})
	if len(got.Features) != 4 || got.Features[0] != "factory.pipeline_edit" || got.Features[1] != "factory.pipeline_edit_runtime" || got.Features[2] != "factory.pipeline_edit_structure" ||
		got.Features[3] != "factory.pipeline_edit_rules" {
		t.Fatalf("an editing engine announced %v", got.Features)
	}
	// An editor from before the structural edits announces what it did then.
	if got := withFactoryPipelineEditFeature(Hello{}, &runtimeOnlyEditEngine{}); len(got.Features) != 2 || got.Features[1] != "factory.pipeline_edit_runtime" {
		t.Fatalf("a runtime-only editor announced %v", got.Features)
	}
	e := &pipelineEditEngine{}
	if h := withFactoryPipelinePRFeature(withFactoryReposManageFeature(withFactoryLinksFeature(withFactoryAdminFeature(Hello{}, e), e), e), e); len(h.Features) != 0 {
		t.Fatalf("editing implied another feature: %v", h.Features)
	}
	for _, other := range []string{FeatureFactoryAdmin, FeatureFactoryLinks, FeatureFactoryReposManage, FeatureFactoryPipelinePR, FeatureFactoryPipelineEdit, FeatureFactoryPipelineEditRuntime} {
		if FeatureFactoryPipelineEditStructure == other {
			t.Fatal("the structure feature is not distinct")
		}
	}
	for _, other := range []string{FeatureFactoryAdmin, FeatureFactoryLinks, FeatureFactoryReposManage, FeatureFactoryPipelinePR, FeatureFactoryPipelineEdit} {
		if FeatureFactoryPipelineEditRuntime == other {
			t.Fatal("the runtime feature is not distinct")
		}
	}
	for _, other := range []string{FeatureFactoryAdmin, FeatureFactoryLinks, FeatureFactoryReposManage, FeatureFactoryPipelinePR} {
		if FeatureFactoryPipelineEdit == other {
			t.Fatal("the feature is not distinct")
		}
	}
}

func TestFactoryPipelineSourceParamsAreExactlyFactoryAndRepo(t *testing.T) {
	bad := []string{
		`{}`, `[]`, `null`, `"x"`, `{"factory":"f"}`, `{"repo":"a/b"}`, `{"factory":" ","repo":"a/b"}`, `{"factory":"f","repo":""}`,
		`{"factory":"f","repo":"a/b","ref":"main"}`, `{"factory":"f","repo":"a/b","path":".codeaf/factory.yaml"}`, `{"factory":"f","repo":"a/b","url":"https://x"}`,
		`{"factory":"f","repo":"a/b","token":"ghp_x"}`, `{"factory":"f","repo":"a/b","sha":"` + editSHA + `"}`, `{"factory":"f","repo":"a/b","branch":"x"}`,
		`{"factory":"f","repo":"a/b","personal":true}`, `{"factory":"f","repo":"a/b"} {"x":1}`,
		`{"factory":"f","repo":"ab"}`, `{"factory":"f","repo":"a/b/c"}`, `{"factory":"f","repo":"../b"}`, `{"factory":"f","repo":"a/.."}`,
		`{"factory":"f","repo":"a/b?x=1"}`, `{"factory":"f","repo":"a/b#x"}`, `{"factory":"f","repo":"a/é"}`, `{"factory":"f","repo":"a/%2e%2e"}`,
		`{"factory":"f","repo":"a/b\u0000"}`, `{"factory":"f","repo":"` + strings.Repeat("o", RepoOwnerMax+1) + `/b"}`,
	}
	eng := &pipelineEditEngine{}
	for _, raw := range bad {
		if _, err := methods["factory.pipeline_source"].call(eng, json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %.100s", raw)
		} else if wireCode(t, err) != CodeBadParams {
			t.Errorf("%.100s: %v", raw, err)
		}
	}
	if len(eng.sources) != 0 {
		t.Fatalf("a bad request reached the engine: %d", len(eng.sources))
	}
	if _, err := methods["factory.pipeline_source"].call(eng, json.RawMessage(` {"factory":" f ","repo":" Acme/api-2.x_y "} `)); err != nil || len(eng.sources) != 1 ||
		eng.sources[0] != (FactoryPipelineSourceParams{Factory: "f", Repo: "Acme/api-2.x_y"}) {
		t.Fatalf("%v %+v", err, eng.sources)
	}
}

func TestFactoryPipelinePreviewParamsAreStrictAndBounded(t *testing.T) {
	long := strings.Repeat("x", 8<<10+1)
	many := "[" + strings.TrimSuffix(strings.Repeat(`{"op":"set_budget","value":3},`, PipelinePreviewEditsMax+1), ",") + "]"
	ok := `[{"op":"set_budget","value":3}]`
	bad := map[string]string{
		"nothing":            `{}`,
		"not an object":      `[]`,
		"two objects":        editGoodPreview(ok) + ` {}`,
		"no edits":           editGoodPreview(`[]`),
		"null edits":         editGoodPreview(`null`),
		"too many edits":     editGoodPreview(many),
		"unknown verb":       editGoodPreview(`[{"op":"reorder","name":"write"}]`),
		"add a step":         editGoodPreview(`[{"op":"add_step","name":"lint","prompt":"x"}]`),
		"remove a step":      editGoodPreview(`[{"op":"remove_step","name":"lint"}]`),
		"no verb":            editGoodPreview(`[{"name":"write","prompt":"x"}]`),
		"unknown edit field": editGoodPreview(`[{"op":"set_budget","value":3,"model":"x"}]`),
		"effort":             editGoodPreview(`[{"op":"set_step","name":"write","effort":"high"}]`),
		"model":              editGoodPreview(`[{"op":"set_step","name":"write","model":"gpt"}]`),
		"only_if":            editGoodPreview(`[{"op":"set_step","name":"write","only_if":"x"}]`),
		"position":           editGoodPreview(`[{"op":"set_step","name":"write","after":"plan"}]`),
		"step budget":        editGoodPreview(`[{"op":"set_step","name":"write","budget":2}]`),
		"set_step no change": editGoodPreview(`[{"op":"set_step","name":"write"}]`),
		"set_step no name":   editGoodPreview(`[{"op":"set_step","prompt":"x"}]`),
		"set_step upper":     editGoodPreview(`[{"op":"set_step","name":"Write","prompt":"x"}]`),
		"set_step path":      editGoodPreview(`[{"op":"set_step","name":"../write","prompt":"x"}]`),
		"set_step value":     editGoodPreview(`[{"op":"set_step","name":"write","prompt":"x","value":3}]`),
		"set_budget name":    editGoodPreview(`[{"op":"set_budget","name":"write","value":3}]`),
		"set_budget prompt":  editGoodPreview(`[{"op":"set_budget","prompt":"x","value":3}]`),
		"set_budget empty":   editGoodPreview(`[{"op":"set_budget"}]`),
		"budget zero":        editGoodPreview(`[{"op":"set_budget","value":0}]`),
		"budget negative":    editGoodPreview(`[{"op":"set_budget","value":-1}]`),
		"budget huge":        editGoodPreview(`[{"op":"set_budget","value":1e9}]`),
		"budget a string":    editGoodPreview(`[{"op":"set_budget","value":"3"}]`),
		"max zero":           editGoodPreview(`[{"op":"set_step","name":"write","max":0}]`),
		"max fraction":       editGoodPreview(`[{"op":"set_step","name":"write","max":1.5}]`),
		"max huge":           editGoodPreview(`[{"op":"set_step","name":"write","max":100}]`),
		"max a string":       editGoodPreview(`[{"op":"set_step","name":"write","max":"2"}]`),
		"prompt blank":       editGoodPreview(`[{"op":"set_step","name":"write","prompt":"  "}]`),
		"prompt long":        editGoodPreview(`[{"op":"set_step","name":"write","prompt":"` + long + `"}]`),
		"prompt nul":         editGoodPreview(`[{"op":"set_step","name":"write","prompt":"a\u0000b"}]`),
		"prompt a number":    editGoodPreview(`[{"op":"set_step","name":"write","prompt":5}]`),
		"until two lines":    editGoodPreview(`[{"op":"set_step","name":"write","until":"a\nb"}]`),
		"until long":         editGoodPreview(`[{"op":"set_step","name":"write","until":"` + strings.Repeat("u", 201) + `"}]`),
		"no base":            `{"factory":"f","repo":"a/b","edits":` + ok + `}`,
		"no base sha":        `{"factory":"f","repo":"a/b","base_text":"x: 1","edits":` + ok + `}`,
		"short sha":          strings.Replace(editGoodPreview(ok), editSHA, "abc", 1),
		"sha is a branch":    strings.Replace(editGoodPreview(ok), editSHA, "main", 1),
		"blank base":         strings.Replace(editGoodPreview(ok), `pipeline: {}\n`, `   \n`, 1),
		"huge base":          strings.Replace(editGoodPreview(ok), `pipeline: {}\n`, strings.Repeat("a", PipelinePreviewTextMax+1), 1),
		"no repo":            `{"factory":"f","base_sha":"` + editSHA + `","base_text":"x: 1","edits":` + ok + `}`,
		"bad repo":           strings.Replace(editGoodPreview(ok), `"a/b"`, `"a/b/c"`, 1),
		"token":              strings.Replace(editGoodPreview(ok), `"edits"`, `"token":"ghp_x","edits"`, 1),
		"path":               strings.Replace(editGoodPreview(ok), `"edits"`, `"path":".codeaf/factory.yaml","edits"`, 1),
		"text":               strings.Replace(editGoodPreview(ok), `"edits"`, `"text":"x","edits"`, 1),
		"title":              strings.Replace(editGoodPreview(ok), `"edits"`, `"title":"x","edits"`, 1),
	}
	eng := &pipelineEditEngine{}
	for name, raw := range bad {
		if _, err := methods["factory.pipeline_preview"].call(eng, json.RawMessage(raw)); err == nil {
			t.Errorf("%s: accepted %.120s", name, raw)
		} else if wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(eng.previews) != 0 {
		t.Fatalf("a bad request reached the engine: %d", len(eng.previews))
	}
}

func TestFactoryPipelinePreviewGoodParamsArePassedOnceAndExactly(t *testing.T) {
	eng := &pipelineEditEngine{}
	upper := strings.ToUpper(editSHA)
	base := "# keep me\n  pipeline: {}\n\n"
	raw, _ := json.Marshal(map[string]any{
		"factory": " f ", "repo": " Acme/api ", "base_sha": " " + upper + " ", "base_text": base,
		"edits": []map[string]any{
			{"op": "set_step", "name": "write", "prompt": "  keep my spaces  ", "until": "", "max": 4},
			{"op": "set_budget", "value": 2.5},
		},
	})
	out, err := methods["factory.pipeline_preview"].call(eng, raw)
	if err != nil || len(eng.previews) != 1 {
		t.Fatalf("%v after %d calls", err, len(eng.previews))
	}
	got := eng.previews[0]
	if got.Factory != "f" || got.Repo != "Acme/api" || got.BaseSHA != editSHA || got.BaseText != base || len(got.Edits) != 2 {
		t.Fatalf("%+v", got)
	}
	e0 := got.Edits[0]
	if e0.Op != "set_step" || e0.Name != "write" || e0.Prompt == nil || *e0.Prompt != "  keep my spaces  " || e0.Until == nil || *e0.Until != "" || e0.Max == nil || *e0.Max != 4 || e0.Value != nil {
		t.Fatalf("edits are forwarded exactly: %+v", e0)
	}
	if e1 := got.Edits[1]; e1.Op != "set_budget" || e1.Value == nil || *e1.Value != 2.5 || e1.Name != "" || e1.Prompt != nil {
		t.Fatalf("%+v", e1)
	}
	data, _ := json.Marshal(out)
	var keys map[string]any
	_ = json.Unmarshal(data, &keys)
	steps, hasSteps := keys["steps"].([]any)
	if keys["factory"] != "f" || keys["repo"] != "Acme/api" || keys["base_sha"] != editSHA || !hasSteps || len(steps) != 0 || keys["changed"] != false {
		t.Fatalf("receipt = %s", data)
	}
	// The largest allowed request fits.
	maxEdits := make([]FactoryPipelineEdit, PipelinePreviewEditsMax)
	for i := range maxEdits {
		maxEdits[i] = FactoryPipelineEdit{Op: "set_step", Name: "write", Prompt: strPtr(strings.Repeat("p", 8<<10))}
	}
	if _, err := CheckPipelinePreview(FactoryPipelinePreviewParams{Factory: "f", Repo: "a/b", BaseSHA: editSHA, BaseText: strings.Repeat("x", PipelinePreviewTextMax), Edits: maxEdits}); err != nil {
		t.Fatal(err)
	}
}

func strPtr(s string) *string { return &s }

// An engine's refusal is passed on whole, with its Detail, and asked once.
func TestFactoryPipelineEditPassesTheEnginesErrorAndAsksOnce(t *testing.T) {
	for _, code := range []string{CodeConflict, CodeForbidden, CodeSignedOut, CodeGitHubExpired, CodeUnreachable, CodeNotFound, CodeBadParams, CodeEngine} {
		e := Errorf(code, "words for %s", code)
		e.Detail = FactoryPipelineEditRefusal{Factory: "f", Repo: "a/b", Code: "locked"}
		eng := &pipelineEditEngine{err: e}
		_, err := methods["factory.pipeline_source"].call(eng, json.RawMessage(editGoodSource))
		_, err2 := methods["factory.pipeline_preview"].call(eng, json.RawMessage(editGoodPreview(`[{"op":"set_budget","value":3}]`)))
		for _, got := range []error{err, err2} {
			if wireCode(t, got) != code {
				t.Errorf("%s: %v", code, got)
			}
			var w *Error
			if !errors.As(got, &w) || w.Detail == nil {
				t.Errorf("%s lost its detail", code)
			}
		}
		if len(eng.sources) != 1 || len(eng.previews) != 1 {
			t.Errorf("%s asked %d and %d times", code, len(eng.sources), len(eng.previews))
		}
	}
}

// THE FIELDS ARE EXACTLY THOSE DOCUMENTED, nothing about models, efforts, only_if,
// positions or reviewers is on the wire, and the schema names both methods and
// the capability.
func TestFactoryPipelineEditSchemaIsTheDocumentedFieldsAndNothingElse(t *testing.T) {
	for ty, want := range map[reflect.Type][]string{
		reflect.TypeFor[FactoryPipelineSourceParams](): {"factory", "repo"},
		reflect.TypeFor[FactoryPipelineSource](): {"factory", "repo", "path", "default_branch", "base_sha", "can_push", "archived", "runtime_fields", "state", "editable", "text", "problems",
			"default_budget", "budget_in_file", "pipelines", "steps", "fixed", "rules", "ask_first", "never", "adapt", "labels", "max_size", "max_cost"},
		reflect.TypeFor[FactoryPipelineEdit]():          {"op", "name", "pipeline", "after", "kind", "prompt", "until", "max", "when", "effort", "fanout", "value", "list", "lines", "adapt", "fixed", "max_size", "max_cost"},
		reflect.TypeFor[FactoryPipelinePreviewParams](): {"factory", "repo", "base_sha", "base_text", "edits"},
		reflect.TypeFor[FactoryPipelineStepValues]():    {"prompt", "until", "max", "when", "effort", "fanout"},
		reflect.TypeFor[FactoryPipelineStepChange]():    {"name", "fields", "before", "after"},
		reflect.TypeFor[FactoryPipelineBudgetChange]():  {"before", "after"},
		reflect.TypeFor[FactoryPipelinePreview]():       {"factory", "repo", "base_sha", "path", "text", "changed", "format_rewritten", "comments_dropped", "steps", "budget", "structure", "lists", "settings"},
		reflect.TypeFor[FactoryPipelineListChange]():    {"list", "before", "after"},
		reflect.TypeFor[FactoryPipelineSettingChange](): {"setting", "before", "after"},
		reflect.TypeFor[FactoryPipelineStructure]():     {"order", "added", "counts", "gates"},
		reflect.TypeFor[FactoryPipelineOrderChange]():   {"pipeline", "before", "after"},
		reflect.TypeFor[FactoryPipelineCount]():         {"pipeline", "steps"},
		reflect.TypeFor[FactoryPipelineGateChange]():    {"pipeline", "gate", "before", "after"},
		reflect.TypeFor[FactoryPipelineEditRefusal]():   {"factory", "repo", "code", "op"},
	} {
		if got := fieldNames(ty); !reflect.DeepEqual(got, want) {
			t.Errorf("%s fields: %v, want %v", ty.Name(), got, want)
		}
	}
	doc := Schema()["methods"].(map[string]any)
	for _, name := range []string{"factory.pipeline_source", "factory.pipeline_preview"} {
		entry, ok := doc[name].(map[string]any)
		if !ok || entry["params"] == nil || entry["result"] == nil {
			t.Fatalf("schema has no entry for %s", name)
		}
	}
	defs := Schema()["$defs"].(map[string]any)
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["pipeline_edit_runtime"] == nil {
		t.Fatal("capabilities do not carry pipeline_edit_runtime")
	}
	if caps["pipeline_edit"] == nil {
		t.Fatal("capabilities do not carry pipeline_edit")
	}
	if caps["pipeline_edit_structure"] == nil {
		t.Fatal("capabilities do not carry pipeline_edit_structure")
	}
	if caps["pipeline_edit_rules"] == nil {
		t.Fatal("capabilities do not carry pipeline_edit_rules")
	}
	// The six verbs, and no loop, reorder or kind verb.
	def, ok := defs["in.FactoryPipelineEdit"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no definition of an edit: %v", defs)
	}
	edit := def["properties"].(map[string]any)
	op, _ := json.Marshal(edit["op"])
	for _, verb := range []string{"set_step", "set_budget", "add_step", "remove_step", "move_step", "set_list"} {
		if !strings.Contains(string(op), `"`+verb+`"`) {
			t.Fatalf("op = %s lacks %s", op, verb)
		}
	}
	if strings.Contains(string(op), "loop") || strings.Contains(string(op), "reorder") || strings.Contains(string(op), "kind") || strings.Count(string(op), "_step") != 4 {
		t.Fatalf("op = %s", op)
	}
}

func TestFactoryPipelineEditBoundsAreTheRoutesOwn(t *testing.T) {
	if PipelinePreviewTextMax != PipelinePRTextMax {
		t.Fatalf("the preview takes %d bytes of base and the pull request route %d", PipelinePreviewTextMax, PipelinePRTextMax)
	}
}

// runtime_fields is on the source whatever it says, false included, and the
// steps a source carries have no runtime key to write unless they hold a word.
func TestFactoryPipelineSourceAlwaysSaysRuntimeFields(t *testing.T) {
	for _, v := range []bool{true, false} {
		raw, err := json.Marshal(FactoryPipelineSource{RuntimeFields: v, Steps: []api.PipelineEditStep{{Name: "a"}, {Name: "b", Kind: "check", When: "thin", Effort: "cheap", Fanout: "per-file"}}})
		if err != nil {
			t.Fatal(err)
		}
		want := `"runtime_fields":` + fmt.Sprint(v)
		if !strings.Contains(string(raw), want) || !strings.Contains(string(raw), `{"name":"a","locked":false,"in_file":false}`) ||
			!strings.Contains(string(raw), `"kind":"check","when":"thin","effort":"cheap","fanout":"per-file"`) {
			t.Fatalf("%s", raw)
		}
	}
}

func TestRuntimeEditFieldsDecodeAndKindIsUnknown(t *testing.T) {
	e := &pipelineEditEngine{}
	ok := editGoodPreview(`[{"op":"set_step","name":"plan","when":"thin","effort":"","fanout":"per-file"}]`)
	if _, err := methods["factory.pipeline_preview"].call(e, json.RawMessage(ok)); err != nil {
		t.Fatal(err)
	}
	if len(e.previews) != 1 || e.previews[0].Edits[0].When == nil || *e.previews[0].Edits[0].When != "thin" ||
		e.previews[0].Edits[0].Effort == nil || *e.previews[0].Edits[0].Effort != "" || *e.previews[0].Edits[0].Fanout != "per-file" {
		t.Fatalf("%+v", e.previews)
	}
	for _, bad := range []string{`[{"op":"set_step","name":"plan","kind":"chat"}]`, `[{"op":"set_step","name":"plan","kind":""}]`} {
		if _, err := methods["factory.pipeline_preview"].call(e, json.RawMessage(editGoodPreview(bad))); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestCheckPipelinePreviewBoundsTheRuntimeWords(t *testing.T) {
	str := func(s string) *string { return &s }
	v := 3.0
	base := FactoryPipelinePreviewParams{Factory: "f", Repo: "a/b", BaseSHA: editSHA, BaseText: "pipeline: {}\n"}
	for name, c := range map[string]struct {
		e       FactoryPipelineEdit
		wantErr string
	}{
		"bad when":      {FactoryPipelineEdit{Op: "set_step", Name: "plan", When: str("sometimes")}, "edit 0"},
		"bad effort":    {FactoryPipelineEdit{Op: "set_step", Name: "plan", Effort: str("max")}, "edit 0"},
		"bad fanout":    {FactoryPipelineEdit{Op: "set_step", Name: "plan", Fanout: str("many")}, "edit 0"},
		"clear effort":  {FactoryPipelineEdit{Op: "set_step", Name: "plan", Effort: str("")}, ""},
		"clear fanout":  {FactoryPipelineEdit{Op: "set_step", Name: "plan", Fanout: str("")}, ""},
		"budget fanout": {FactoryPipelineEdit{Op: "set_budget", Value: &v, Fanout: str("one")}, "edit 0"},
	} {
		p := base
		p.Edits = []FactoryPipelineEdit{c.e}
		_, err := CheckPipelinePreview(p)
		if (c.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPipelineEditOpsNeverSetKind(t *testing.T) {
	s := func(v string) *string { return &v }
	for _, w := range []*string{nil, s("thin")} {
		for _, f := range []*string{nil, s("")} {
			ops := PipelineEditOps([]FactoryPipelineEdit{{Op: "set_step", Name: "plan", When: w, Effort: f, Fanout: w}})
			if ops[0].Kind != nil || ops[0].When != w || ops[0].Effort != f || ops[0].Fanout != w {
				t.Fatalf("%+v", ops[0])
			}
		}
	}
}

func TestStepValuesOmitEmptyRuntimeWords(t *testing.T) {
	raw, _ := json.Marshal(FactoryPipelineStepValues{Prompt: "p", Until: "u", Max: 2})
	if string(raw) != `{"prompt":"p","until":"u","max":2}` {
		t.Fatal(string(raw))
	}
	raw, _ = json.Marshal(FactoryPipelineStepValues{Prompt: "p", When: "thin", Fanout: "one"})
	if !strings.Contains(string(raw), `"when":"thin"`) || !strings.Contains(string(raw), `"fanout":"one"`) || strings.Contains(string(raw), "effort") {
		t.Fatal(string(raw))
	}
}

// PipelineRuntimeGuarded makes the test engine a guarded editor, like appBackend.
func (e *pipelineEditEngine) PipelineRuntimeGuarded() {}

// PipelineStructureEdits makes the test engine a structural editor, like appBackend.
func (e *pipelineEditEngine) PipelineStructureEdits() {}

// PipelineRulesEdits makes the test engine a rules editor, like appBackend.
func (e *pipelineEditEngine) PipelineRulesEdits() {}

// runtimeOnlyEditEngine is an editor from before the structural edits: it
// guards the runtime words and has no structure marker.
type runtimeOnlyEditEngine struct {
	factoryEngine
	FactoryPipelineEditor
}

func (*runtimeOnlyEditEngine) PipelineRuntimeGuarded() {}

// unguardedEditEngine has every method of an editor and no guard marker.
type unguardedEditEngine struct {
	factoryEngine
	FactoryPipelineEditor
}
