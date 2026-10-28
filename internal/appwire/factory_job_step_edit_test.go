package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// stepEditEngine is a factory engine that also carries per-job step edits and
// keeps what it was handed.
type stepEditEngine struct {
	factoryEngine
	reads  []FactoryJobStepEditsParams
	writes []FactoryJobStepEditParams
}

func (s *stepEditEngine) FactoryJobStepEditsRead(_ context.Context, p FactoryJobStepEditsParams) (FactoryJobStepEdits, error) {
	s.reads = append(s.reads, p)
	return FactoryJobStepEdits{Job: p.Job, Version: 4}, nil
}

func (s *stepEditEngine) FactoryJobStepEdit(_ context.Context, p FactoryJobStepEditParams) (FactoryJobStepEdited, error) {
	s.writes = append(s.writes, p)
	return FactoryJobStepEdited{Job: p.Job, Version: 5}, nil
}

const stepGuard = `"expect":{"version":4}`

func TestJobStepEditMethodsAreRegisteredAndAbsentWithoutTheEditor(t *testing.T) {
	good := map[string]string{
		"factory.job_step_edits_read": `{"factory":"f","job":2}`,
		"factory.job_step_edit":       `{"factory":"f","job":2,` + stepGuard + `,"edit":{"ask":{"build":"do it"}}}`,
	}
	if len(factoryStepEditMethods) != len(good) {
		t.Fatalf("methods %v, requests %v", factoryStepEditMethods, good)
	}
	for name, params := range good {
		if _, ok := methods[name]; !ok {
			t.Fatalf("%s is not registered", name)
		}
		// Neither a plain engine, a factory reader, nor a FactoryActor is an editor.
		for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &actorEngine{}} {
			_, err := methods[name].call(e, json.RawMessage(params))
			if wireCode(t, err) != CodeUnknownMethod {
				t.Errorf("%s on %T: %v", name, e, err)
			}
		}
		if _, err := methods[name].call(&stepEditEngine{}, json.RawMessage(params)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, e := range []Engine{&factoryEngine{}, &actorEngine{}} {
		if h := withFactoryJobStepEditFeature(Hello{}, e); len(h.Features) != 0 {
			t.Fatalf("%T announced %v", e, h.Features)
		}
	}
	// A non-factory engine that has the methods announces nothing either.
	if h := withFactoryJobStepEditFeature(Hello{}, &onlyEditor{}); len(h.Features) != 0 {
		t.Fatalf("a non-factory engine announced %v", h.Features)
	}
	h := withFactoryJobStepEditFeature(Hello{}, &stepEditEngine{})
	if len(h.Features) != 2 || h.Features[0] != FeatureFactoryJobStepEdit || h.Features[1] != FeatureFactoryJobStepEditChecks {
		t.Fatalf("an editor announced %v", h.Features)
	}
	// The feature is independent of factory.job_actions.
	if withFactoryActorFeature(Hello{}, &stepEditEngine{}).Features != nil {
		t.Fatal("an editor is not an actor")
	}
}

type onlyEditor struct{ fakeEngine }

func (*onlyEditor) FactoryJobStepEditsRead(context.Context, FactoryJobStepEditsParams) (FactoryJobStepEdits, error) {
	return FactoryJobStepEdits{}, nil
}
func (*onlyEditor) FactoryJobStepEdit(context.Context, FactoryJobStepEditParams) (FactoryJobStepEdited, error) {
	return FactoryJobStepEdited{}, nil
}

// A WRITE WITH NO VERSION, OR A PART THIS WIRE DOES NOT CARRY, NEVER REACHES THE ENGINE.
func TestJobStepEditRefusesBadRequestsBeforeTheEngine(t *testing.T) {
	eng := &stepEditEngine{}
	long := strings.Repeat("x", 241)
	w := func(edit string) string {
		return `{"factory":"f","job":2,` + stepGuard + `,"edit":` + edit + `}`
	}
	bad := []struct{ name, params string }{
		// No version read.
		{"factory.job_step_edit", `{"factory":"f","job":2,"edit":{"skip":["build"]}}`},
		{"factory.job_step_edit", `{"factory":"f","job":2,"expect":{},"edit":{"skip":["build"]}}`},
		{"factory.job_step_edit", `{"factory":"f","job":2,"expect":{"version":0},"edit":{"skip":["build"]}}`},
		{"factory.job_step_edit", `{"factory":"f","job":0,` + stepGuard + `,"edit":{"skip":["build"]}}`},
		{"factory.job_step_edits_read", `{"factory":"f","job":0}`},
		{"factory.job_step_edits_read", `{"factory":"","job":1}`},
		// Nothing to change.
		{"factory.job_step_edit", w(`{}`)},
		{"factory.job_step_edit", w(`{"why":"just because"}`)},
		// Unknown fields, top level and nested: strict, not ignored.
		{"factory.job_step_edit", `{"factory":"f","job":2,` + stepGuard + `,"edit":{"skip":["a"]},"path":"/v1/jobs/3/step-edits"}`},
		{"factory.job_step_edit", `{"factory":"f","job":2,` + stepGuard + `,"edit":{"skip":["a"],"remove":["a"]}}`},
		{"factory.job_step_edit", `{"factory":"f","job":2,` + stepGuard + `,"edit":{"skip":["a"]},"by":"manager"}`},
		{"factory.job_step_edits_read", `{"factory":"f","job":1,"expect":{"version":1}}`},
		// Added steps carry name, kind, ask, until, effort and after only.
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","max":3}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","proof":["go test"]}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","when":"before plan"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","fanout":"files"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","by":"plan"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x"},"before":"plan"}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","path":"/etc/passwd"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","token":"t"}}]}`)},
		// The engine's own bounds.
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"two words","ask":"x"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"a","ask":"x"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","kind":"post","ask":"x"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","kind":"check","ask":"x","max":10}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"x","effort":"max"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"until":"done"}}]}`)},
		{"factory.job_step_edit", w(`{"add":[{"step":{"name":"lint","ask":"` + long + `"}}]}`)},
		{"factory.job_step_edit", w(`{"ask":{"build":"` + long + `"}}`)},
		{"factory.job_step_edit", w(`{"ask":{"build":"  "}}`)},
		{"factory.job_step_edit", w(`{"thinking":{"build":"wild"}}`)},
		{"factory.job_step_edit", w(`{"skip":[""]}`)},
		{"factory.job_step_edit", w(`{"on":["a\u0000b"]}`)},
		{"factory.job_step_edit", w(`{"skip":["a","b","c","d","e","f","g","h","i","j"]}`)},
		{"factory.job_step_edit", w(`{"skip":["a"],"why":"` + strings.Repeat("y", MaxFactoryJobWords+1) + `"}`)},
	}
	for _, c := range bad {
		_, err := methods[c.name].call(eng, json.RawMessage(c.params))
		if wireCode(t, err) != CodeBadParams {
			t.Errorf("%s %s: %v", c.name, c.params, err)
		}
	}
	if len(eng.reads)+len(eng.writes) != 0 {
		t.Fatalf("a refused request reached the engine: %v %v", eng.reads, eng.writes)
	}
}

func TestJobStepEditPassesTrimmedTypedArguments(t *testing.T) {
	eng := &stepEditEngine{}
	params := `{"factory":" f ","job":2,"expect":{"version":4,"state":"running"},"edit":{
		"add":[{"step":{"name":" Lint ","kind":"chat","ask":"  run   the linter ","until":" clean ","effort":" Cheap "},"after":" build "},{"step":{"ask":"look around"}}],
		"ask":{" build ":" do\nit "},"thinking":{"build":" STRONG ","plan":""},"on":[" review "],"skip":["docs"],"why":" faster\tplease "}}`
	if _, err := methods["factory.job_step_edit"].call(eng, json.RawMessage(params)); err != nil {
		t.Fatal(err)
	}
	p := eng.writes[0]
	if p.Factory != "f" || p.Job != 2 || *p.Expect.Version != 4 || p.Expect.State != "running" {
		t.Fatalf("guard: %+v", p)
	}
	e := p.Edit
	first := e.Add[0]
	if first.After != "build" || first.Step != (FactoryNewStep{Name: "Lint", Kind: "chat", Ask: "run the linter", Until: "clean", Effort: "cheap"}) {
		t.Fatalf("add: %+v", e.Add)
	}
	if e.Ask["build"] != "do it" || e.Thinking["build"] != "strong" || e.Thinking["plan"] != "" || len(e.Thinking) != 2 {
		t.Fatalf("maps: %+v %+v", e.Ask, e.Thinking)
	}
	if len(e.On) != 1 || e.On[0] != "review" || e.Skip[0] != "docs" || e.Why != "faster please" {
		t.Fatalf("lists: %+v", e)
	}
	// A gate (approval) step needs no ask.
	if _, err := methods["factory.job_step_edit"].call(eng, json.RawMessage(`{"factory":"f","job":2,`+stepGuard+`,"edit":{"add":[{"step":{"name":"signoff","kind":"gate"},"after":"plan"}]}}`)); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if _, err := methods["factory.job_step_edits_read"].call(eng, json.RawMessage(`{"factory":" f ","job":2}`)); err != nil || eng.reads[0].Factory != "f" {
		t.Fatalf("read: %v %+v", err, eng.reads)
	}
}

func TestJobStepEditKindOnlyGateMatchesFactoryAndTrimCollisionsSendNothing(t *testing.T) {
	for _, raw := range []string{
		`{"factory":"f","job":2,"expect":{"version":4},"edit":{"add":[{"step":{"kind":"gate"},"after":"plan"}]}}`,
		`{"factory":"f","job":2,"expect":{"version":4},"edit":{"add":[{"step":{"kind":"gate","name":"sign-off"},"after":"plan"}]}}`,
	} {
		eng := &stepEditEngine{}
		if _, err := methods["factory.job_step_edit"].call(eng, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
		if len(eng.writes) != 1 {
			t.Fatalf("gate not carried: %+v", eng.writes)
		}
	}
	for _, part := range []string{
		`"ask":{"write":"one"," write ":"two"}`,
		`"thinking":{"write":"cheap"," write ":"strong"}`,
	} {
		eng := &stepEditEngine{}
		raw := `{"factory":"f","job":2,"expect":{"version":4},"edit":{` + part + `}}`
		_, err := methods["factory.job_step_edit"].call(eng, json.RawMessage(raw))
		if err == nil || wireCode(t, err) != CodeBadParams {
			t.Fatalf("duplicate silently collapsed: %v", err)
		}
		if len(eng.writes) != 0 {
			t.Fatalf("duplicate reached factory: %+v", eng.writes)
		}
	}
}
