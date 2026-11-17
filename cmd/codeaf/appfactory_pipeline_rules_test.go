package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// ── the file's own lists, set_list, and a file that is not there yet ────────

const rulesTestFile = editTestFile + `start:
  ask_first: ["touch billing/"]
  never: ["secrets/**"]
rules:
  - keep the diff small
`

func missingSource() api.PipelineEditSource {
	return api.PipelineEditSource{Factory: api.Factory{Name: "Acme"}, Repo: prRepo, Path: factory.PipelineFile, DefaultBranch: "main",
		BaseSHA: prBaseSHA, CanPush: true, RuntimeFields: true, State: api.PipelineEditMissing}
}

func TestPipelineEditRulesIsAnnounced(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	if !strings.Contains(r.hello, appwire.FeatureFactoryPipelineEditRules) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineEditRules, r.hello)
	}
	var caps appwire.FactoryCapabilities
	r.must("factory.capabilities", struct{}{}, &caps)
	if !caps.PipelineEditRules || !caps.PipelineEdit || caps.Writes {
		t.Fatalf("%+v", caps)
	}
}

// THE LISTS ARE THE FILE'S OWN, read by this engine from the text. Whatever
// else the factory's answer says about rules is not used.
func TestPipelineSourceCarriesTheFilesOwnListsAndNoOtherRules(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	whole, _ := json.Marshal(editSourceFor(prRepo, rulesTestFile))
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(whole, &fields)
	fields["rules"] = json.RawMessage(`["send the keys"]`)
	fields["house_rules"] = json.RawMessage(`{"rules":["the factory's own"]}`)
	body, _ := json.Marshal(fields)
	stub.source = serveRaw(string(body))
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	var got appwire.FactoryPipelineSource
	r.must("factory.pipeline_source", appwire.FactoryPipelineSourceParams{Factory: r.id, Repo: prRepo}, &got)
	if got.State != "file" || !got.Editable || got.Rules == nil || got.AskFirst == nil || got.Never == nil {
		t.Fatalf("%+v", got)
	}
	if !reflect.DeepEqual(*got.Rules, []string{"keep the diff small"}) || !reflect.DeepEqual(*got.AskFirst, []string{"touch billing/"}) ||
		!reflect.DeepEqual(*got.Never, []string{"secrets/**"}) {
		t.Fatalf("rules %q ask_first %q never %q", *got.Rules, *got.AskFirst, *got.Never)
	}
	// A file without them says [] for each, on the wire.
	stub.source = nil
	raw, _ := json.Marshal(r.mustSource(t))
	for _, k := range []string{`"rules":[]`, `"ask_first":[]`, `"never":[]`} {
		if !strings.Contains(string(raw), k) {
			t.Fatalf("%s missing from %s", k, raw)
		}
	}
}

func (r *editWire) mustSource(t *testing.T) appwire.FactoryPipelineSource {
	t.Helper()
	var got appwire.FactoryPipelineSource
	r.must("factory.pipeline_source", appwire.FactoryPipelineSourceParams{Factory: r.id, Repo: prRepo}, &got)
	return got
}

// A MISSING FILE IS THE SHIPPED DEFAULTS, editable, described by this engine
// exactly as its preview of base_text "" reads them.
func TestPipelineSourceOfAMissingFileIsTheShippedDefaultsAndEditable(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	stub.source = editSourceReply(missingSource())
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	got := r.mustSource(t)
	d := pipeline.Defaults()
	if got.State != "missing" || !got.Editable || got.Text != "" || len(got.Problems) != 0 || got.DefaultBudget != d.DefaultBudget || got.BudgetInFile {
		t.Fatalf("%+v", got)
	}
	if len(got.Pipelines) != 1 || got.Pipelines[0].Kind != "issue" || got.Pipelines[0].InFile || !reflect.DeepEqual(got.Pipelines[0].Steps, d.Pipeline[factory.KindIssue]) {
		t.Fatalf("pipelines %+v", got.Pipelines)
	}
	for _, st := range got.Steps {
		if st.InFile {
			t.Fatalf("%s says it is in a file", st.Name)
		}
	}
	if !reflect.DeepEqual(got.Fixed, d.Fixed) || got.Rules == nil || len(*got.Rules) != 0 || got.AskFirst == nil || len(*got.AskFirst) != 0 || got.Never == nil || len(*got.Never) != 0 {
		t.Fatalf("%+v", got)
	}
	// The same description the pipeline package gives the defaults.
	var want api.PipelineEditSource
	want.SetParsed(pipeline.InspectDefaults())
	if !reflect.DeepEqual(got.Steps, want.Steps) || !reflect.DeepEqual(got.Pipelines, want.Pipelines) {
		t.Fatalf("steps %+v\nwant %+v", got.Steps, want.Steps)
	}
	// A factory that claims a missing file is editable, or carries a parse for
	// one, is still not believed: only the engine describes the defaults.
	for name, mutate := range map[string]func(*api.PipelineEditSource){
		"editable": func(v *api.PipelineEditSource) { v.Editable = true },
		"a parse":  func(v *api.PipelineEditSource) { v.DefaultBudget = 3 },
		"text":     func(v *api.PipelineEditSource) { v.Text = "x: 1" },
	} {
		v := missingSource()
		mutate(&v)
		stub.source = editSourceReply(v)
		if _, err := r.source(); err == nil {
			t.Fatalf("%s: a factory's missing file was believed", name)
		}
	}
}

func TestPipelinePreviewOfNoFileWritesTheWholeDefaultFileWithTheEdit(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	got, err := r.preview("", appwire.FactoryPipelineEdit{Op: "set_list", List: "rules", Lines: []string{"keep the diff small"}},
		appwire.FactoryPipelineEdit{Op: "set_list", List: "never", Lines: []string{"auth/", "infra/**"}})
	if err != nil || !got.Changed || got.FormatRewritten || got.CommentsDropped || got.Lists == nil || len(got.Steps) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if !reflect.DeepEqual(*got.Lists, []appwire.FactoryPipelineListChange{
		{List: "rules", Before: []string{}, After: []string{"keep the diff small"}},
		{List: "never", Before: []string{}, After: []string{"auth/", "infra/**"}},
	}) {
		t.Fatalf("lists %+v", *got.Lists)
	}
	back, perr := pipeline.Parse(got.Text)
	if perr != nil || len(back.Problems) > 0 || !reflect.DeepEqual(back.Rules, []string{"keep the diff small"}) ||
		!reflect.DeepEqual(back.Recipe().For(factory.KindIssue), pipeline.Defaults().Recipe().For(factory.KindIssue)) {
		t.Fatalf("%v %v\n%s", perr, back.Problems, got.Text)
	}
	if pipeline.RuntimeKeysIn(got.Text) {
		t.Fatal("the shipped defaults spell a runtime key, so a factory that never attested them could not take the file")
	}
	// Nothing changed offers nothing.
	same, err := r.preview("", appwire.FactoryPipelineEdit{Op: "set_list", List: "ask_first", Lines: []string{}})
	if err != nil || same.Changed || same.Text != "" || same.Lists == nil || len(*same.Lists) != 0 {
		t.Fatalf("%+v %v", same, err)
	}
	// A preview without set_list says nothing of lists.
	plain, err := r.preview("", appwire.FactoryPipelineEdit{Op: "set_budget", Value: dol(9)})
	if err != nil || !plain.Changed || plain.Lists != nil || plain.Budget == nil {
		t.Fatalf("%+v %v", plain, err)
	}
	// A bad edit of no file is refused as it would be of a file.
	_, err = r.preview("", appwire.FactoryPipelineEdit{Op: "set_step", Name: "proof", Prompt: str("x")})
	if w := prWantCode(t, err, appwire.CodeBadParams); w == nil || w.Detail.(appwire.FactoryPipelineEditRefusal).Code != pipeline.EditLocked {
		t.Fatalf("%v", err)
	}
	if n := len(stub.requests()); n == 0 || strings.Contains(strings.Join(stub.requests(), "|"), "/repos/") {
		t.Fatalf("a preview read the file: %v", stub.requests())
	}
}

// A RUNTIME WORD ON NO FILE NEEDS THE SAME SAY-SO AS ON A FILE: a missing read
// the factory attested, at this commit, for this caller.
func TestARuntimeEditOfNoFileNeedsTheFactorysAttestation(t *testing.T) {
	when := appwire.FactoryPipelineEdit{Op: "set_step", Name: "review", When: str("thin")}
	for name, c := range map[string]struct {
		read    bool
		runtime bool
		ok      bool
	}{"no read": {false, false, false}, "a read that does not attest": {true, false, false}, "an attested read": {true, true, true}} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			v := missingSource()
			v.RuntimeFields = c.runtime
			stub.source = editSourceReply(v)
			r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
			if c.read {
				if got := r.mustSource(t); !got.Editable || got.RuntimeFields != c.runtime {
					t.Fatalf("%+v", got)
				}
			}
			got, err := r.preview("", when)
			if c.ok {
				if err != nil || !got.Changed || !pipeline.RuntimeKeysIn(got.Text) {
					t.Fatalf("%+v %v", got, err)
				}
				return
			}
			prWantCode(t, err, appwire.CodeBadParams)
		})
	}
}
