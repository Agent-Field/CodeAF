package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// pickerEngine is an actor engine that says it starts a job on a picked
// pipeline, like appBackend.
type pickerEngine struct{ actorEngine }

func (*pickerEngine) FactoryJobPipelineStarts() {}

func TestStartAndCreateTakeAPipelineOnlyFromAnEngineThatSaysSo(t *testing.T) {
	features := func(e Engine) string { return strings.Join(withFactoryActorFeature(Hello{}, e).Features, ",") }
	if got := features(&actorEngine{}); strings.Contains(got, FeatureFactoryStartPipeline) {
		t.Fatalf("an actor without the marker announced %v", got)
	}
	if got := features(&pickerEngine{}); !strings.Contains(got, FeatureFactoryStartPipeline) {
		t.Fatalf("a picker announced %v", got)
	}
	start := `{"factory":"f","job":1,"expect":{"version":1},"pipeline":"docs"}`
	create := `{"factory":"f","repo":"o/r","title":"Fix it","pipeline":"docs"}`
	old := &actorEngine{}
	for name, params := range map[string]string{"factory.job_start": start, "factory.job_create": create} {
		if _, err := methods[name].call(old, json.RawMessage(params)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: a pipeline reached an engine without the feature: %v", name, err)
		}
	}
	if len(old.acts) != 0 {
		t.Fatalf("%v", old.acts)
	}
	eng := &pickerEngine{}
	for name, params := range map[string]string{"factory.job_start": start, "factory.job_create": create} {
		if _, err := methods[name].call(eng, json.RawMessage(params)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, p := range eng.got {
		switch p := p.(type) {
		case FactoryJobStartParams:
			if p.Pipeline != "docs" {
				t.Fatalf("%+v", p)
			}
		case FactoryJobCreateParams:
			if p.Pipeline != "docs" {
				t.Fatalf("%+v", p)
			}
		default:
			t.Fatalf("%T", p)
		}
	}
	for _, bad := range []string{`"Docs"`, `"docs pipeline"`, `"d"`} {
		params := `{"factory":"f","job":1,"expect":{"version":1},"pipeline":` + bad + `}`
		if _, err := methods["factory.job_start"].call(eng, json.RawMessage(params)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
