package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const runtimeSourceFile = `steps:
  plan:
    prompt: plan it
    when: thin
    effort: cheap
    fanout: per-file
  test:
    kind: check
    prompt: go test ./...
`

// runtimeReply writes v the way a factory would, then lets edit shape the JSON:
// an OLD factory is the same reply without the new keys.
func runtimeReply(t *testing.T, v api.PipelineEditSource, edit func(m map[string]any)) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(m)
	}
	raw, _ = json.Marshal(m)
	return string(raw)
}

// oldShape removes everything this change added to the reply.
func oldShape(m map[string]any) {
	delete(m, "runtime_fields")
	steps, _ := m["steps"].([]any)
	for _, s := range steps {
		for _, k := range []string{"kind", "when", "effort", "fanout"} {
			delete(s.(map[string]any), k)
		}
	}
}

func runtimeSource(t *testing.T, body string) (appwire.FactoryPipelineSource, error) {
	t.Helper()
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	stub.source = serveRaw(body)
	return newEditWire(t, stub, false, api.FactoryTeam, prToken).source()
}

func attested(v api.PipelineEditSource) api.PipelineEditSource {
	v.RuntimeFields = true
	return v
}

func TestPipelineSourceOfAnOldShapeLegacyFileIsAcceptedUnattested(t *testing.T) {
	good := editSourceFor(prRepo, editTestFile)
	got, err := runtimeSource(t, runtimeReply(t, good, oldShape))
	if err != nil || !got.Editable || got.RuntimeFields {
		t.Fatalf("%+v %v", got, err)
	}
	// The app is told false, not nothing, and no step grows a runtime key.
	raw, _ := json.Marshal(got)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	if string(m["runtime_fields"]) != "false" {
		t.Fatalf("%s", raw)
	}
	for _, s := range got.Steps {
		if s.Kind != "" || s.When != "" || s.Effort != "" || s.Fanout != "" {
			t.Fatalf("%+v", s)
		}
	}
	// An explicit false is the same claim as none.
	got, err = runtimeSource(t, runtimeReply(t, good, func(m map[string]any) { m["runtime_fields"] = false }))
	if err != nil || !got.Editable || got.RuntimeFields {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPipelineSourceCarriesTheRemotesClaimInEveryAcceptedState(t *testing.T) {
	big := editSourceFor(prRepo, "")
	big.State, big.Text, big.Problems = api.PipelineEditTooLarge, "", nil
	missing := editSourceFor(prRepo, "")
	missing.State, missing.Text, missing.Problems = api.PipelineEditMissing, "", nil
	for name, v := range map[string]api.PipelineEditSource{
		"file": editSourceFor(prRepo, editTestFile), "runtime file": editSourceFor(prRepo, runtimeSourceFile),
		"invalid": editSourceFor(prRepo, "pipeline: ["), "missing": missing, "too large": big,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := runtimeSource(t, runtimeReply(t, attested(v), nil))
			if err != nil || !got.RuntimeFields || got.State != v.State {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestPipelineSourceCopiesAllFourWordsOfAnAttestedRuntimeFile(t *testing.T) {
	got, err := runtimeSource(t, runtimeReply(t, attested(editSourceFor(prRepo, runtimeSourceFile)), nil))
	if err != nil || !got.Editable || !got.RuntimeFields {
		t.Fatalf("%+v %v", got, err)
	}
	by := map[string]api.PipelineEditStep{}
	for _, s := range got.Steps {
		by[s.Name] = s
	}
	if p := by["plan"]; p.When != "thin" || p.Effort != "cheap" || p.Fanout != "per-file" || p.Kind != "" || p.Locked {
		t.Fatalf("%+v", p)
	}
	// A check is not locked: its command is edited like a prompt.
	if s := by["test"]; s.Kind != "check" || s.Locked {
		t.Fatalf("%+v", s)
	}
	raw, _ := json.Marshal(got)
	for _, want := range []string{`"runtime_fields":true`, `"when":"thin"`, `"effort":"cheap"`, `"fanout":"per-file"`, `"kind":"check"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s missing from %s", want, raw)
		}
	}
}

func TestPipelineSourceRefusesARuntimeFileFromAFactoryThatDoesNotSayItRunsIt(t *testing.T) {
	for name, text := range map[string]string{
		"kind check":             "steps:\n  test: {kind: check, prompt: go test ./...}\n",
		"kind chat":              "steps:\n  plan:\n    kind: chat\n    prompt: p\n",
		"when always":            "steps:\n  plan:\n    when: always\n    prompt: p\n",
		"effort":                 "steps:\n  plan:\n    effort: strong\n    prompt: p\n",
		"fanout":                 "steps:\n  plan:\n    fanout: one\n    prompt: p\n",
		"all four":               runtimeSourceFile,
		"an invalid word":        "steps:\n  plan:\n    when: sometimes\n    prompt: p\n",
		"a flow step":            "steps: {plan: {prompt: p, effort: cheap}}\n",
		"a runtime key + legacy": "steps:\n  plan: {prompt: p, max: 2}\n  test: {kind: gate}\n",
	} {
		t.Run(name, func(t *testing.T) {
			v := editSourceFor(prRepo, text)
			shapes := map[string]string{
				// What a factory that predates the words answers: the file is invalid.
				"old remote": runtimeReply(t, func() api.PipelineEditSource {
					o := v
					o.State, o.Editable, o.Steps, o.Pipelines, o.Fixed, o.DefaultBudget = api.PipelineEditInvalid, false, nil, nil, nil, 0
					o.Problems = []string{"steps: a step is a prompt, an until and a max"}
					return o
				}(), oldShape),
				// A factory in between that reads the file but cannot say it runs the words.
				"no claim":    runtimeReply(t, v, oldShape),
				"claim false": runtimeReply(t, v, func(m map[string]any) { m["runtime_fields"] = false }),
			}
			for shape, body := range shapes {
				got, err := runtimeSource(t, body)
				if err == nil || got.Editable || got.Text != "" {
					t.Fatalf("%s: %+v %v", shape, got, err)
				}
				if err.Error() != errPipelineSourceRuntimeUnattested.Error() {
					t.Fatalf("%s: %v", shape, err)
				}
			}
		})
	}
}

func TestPipelineSourceDoesNotTakeWordsInCommentsOrPromptsForKeys(t *testing.T) {
	for name, text := range map[string]string{
		"comment":      "# kind: check\nsteps:\n  plan:\n    prompt: p # when: thin, effort: cheap\n",
		"quoted":       "steps:\n  plan:\n    prompt: \"kind: check; when: thin; fanout: per-file\"\n",
		"block prompt": "steps:\n  plan:\n    prompt: |\n      kind: gate\n      effort: cheap\n",
		"top level":    "start: when thin\nsteps:\n  plan: {prompt: p}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if pipeline.RuntimeKeysIn(text) {
				t.Fatal("a word was taken for a key")
			}
			v := editSourceFor(prRepo, text)
			if !v.Editable {
				t.Skip("not an editable file in this build: " + strings.Join(v.Problems, "; "))
			}
			got, err := runtimeSource(t, runtimeReply(t, v, oldShape))
			if err != nil || !got.Editable || got.RuntimeFields {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestPipelineSourceDoesNotBelieveAClaimTheStepsContradict(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"words missing": func(m map[string]any) {
			for _, s := range m["steps"].([]any) {
				for _, k := range []string{"kind", "when", "effort", "fanout"} {
					delete(s.(map[string]any), k)
				}
			}
		},
		"kind missing":         func(m map[string]any) { stepNamed(m, "test")["kind"] = nil; delete(stepNamed(m, "test"), "kind") },
		"when altered":         func(m map[string]any) { stepNamed(m, "plan")["when"] = "large" },
		"effort dropped":       func(m map[string]any) { delete(stepNamed(m, "plan"), "effort") },
		"fanout altered":       func(m map[string]any) { stepNamed(m, "plan")["fanout"] = "per-claim" },
		"kind chat made check": func(m map[string]any) { stepNamed(m, "plan")["kind"] = "check" },
		"check locked":         func(m map[string]any) { stepNamed(m, "test")["locked"] = true },
		"chat locked":          func(m map[string]any) { stepNamed(m, "plan")["locked"] = true },
		"word invented":        func(m map[string]any) { stepNamed(m, "plan")["fanout"] = "per-file " },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := runtimeSource(t, runtimeReply(t, attested(editSourceFor(prRepo, runtimeSourceFile)), edit))
			if err == nil || err.Error() != errPipelineSourceUnreadable.Error() {
				t.Fatalf("%v", err)
			}
		})
	}
	// The same lie about a legacy file: a claim of true does not license invented words.
	_, err := runtimeSource(t, runtimeReply(t, attested(editSourceFor(prRepo, editTestFile)), func(m map[string]any) { stepNamed(m, "plan")["when"] = "thin" }))
	if err == nil {
		t.Fatal("a factory invented a runtime word on a file that names none")
	}
}

func stepNamed(m map[string]any, name string) map[string]any {
	for _, s := range m["steps"].([]any) {
		if s.(map[string]any)["name"] == name {
			return s.(map[string]any)
		}
	}
	panic("no step " + name)
}

func TestPipelineSourceRefusesAClaimThatIsNotTheLiteralBoolean(t *testing.T) {
	for _, claim := range []string{`"true"`, `"false"`, `1`, `0`, `null`, `"yes"`, `[]`, `{}`, `[true]`} {
		for name, v := range map[string]api.PipelineEditSource{
			"legacy": editSourceFor(prRepo, editTestFile), "runtime": editSourceFor(prRepo, runtimeSourceFile),
		} {
			t.Run(name+" "+claim, func(t *testing.T) {
				body := runtimeReply(t, attested(v), nil)
				body = strings.Replace(body, `"runtime_fields":true`, `"runtime_fields":`+claim, 1)
				if !strings.Contains(body, `"runtime_fields":`+claim) {
					t.Fatalf("the claim was not substituted: %s", body)
				}
				got, err := runtimeSource(t, body)
				if err == nil || err.Error() != errPipelineSourceUnreadable.Error() || got.Editable {
					t.Fatalf("%+v %v", got, err)
				}
			})
		}
	}
}
