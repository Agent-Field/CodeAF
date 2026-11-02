package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// sourceRefEngine also reads a pipeline pull request's head by ref.
type sourceRefEngine struct{ pipelineEditEngine }

func (*sourceRefEngine) PipelineSourceRefs() {}

func TestFactoryPipelineSourceRefFeatureNeedsTheMarker(t *testing.T) {
	if FeatureFactoryPipelineSourceRef != "factory.pipeline_source_ref" {
		t.Fatalf("feature %q", FeatureFactoryPipelineSourceRef)
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &pipelineEditEngine{}} {
		if got := withFactoryPipelineSourceRefFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryPipelineSourceRefFeature(Hello{}, &sourceRefEngine{})
	if len(got.Features) != 1 || got.Features[0] != FeatureFactoryPipelineSourceRef {
		t.Fatalf("a ref-reading engine announced %v", got.Features)
	}
}

// AN ENGINE WITHOUT THE WORD refuses ref as an unknown field and is not asked.
func TestFactoryPipelineSourceRefIsRefusedByAnEngineWithoutIt(t *testing.T) {
	eng := &pipelineEditEngine{}
	_, err := methods["factory.pipeline_source"].call(eng, json.RawMessage(`{"factory":"f","repo":"a/b","ref":"42"}`))
	if wireCode(t, err) != CodeBadParams || len(eng.sources) != 0 {
		t.Fatalf("%v after %d calls", err, len(eng.sources))
	}
}

func TestFactoryPipelineSourceRefShapes(t *testing.T) {
	head := strings.Repeat("ab", 20)
	for name, c := range map[string]struct{ raw, want string }{
		"number":     {`"42"`, "42"},
		"head":       {`"` + head + `"`, head},
		"upper head": {`" ` + strings.ToUpper(head) + ` "`, head},
	} {
		t.Run(name, func(t *testing.T) {
			eng := &sourceRefEngine{}
			if _, err := methods["factory.pipeline_source"].call(eng, json.RawMessage(`{"factory":"f","repo":"a/b","ref":`+c.raw+`}`)); err != nil || len(eng.sources) != 1 {
				t.Fatalf("%v after %d calls", err, len(eng.sources))
			}
			if got := eng.sources[0].Ref; got != c.want {
				t.Fatalf("ref %q, want %q", got, c.want)
			}
		})
	}
	for _, raw := range []string{`42`, `"0"`, `"042"`, `"-1"`, `"+1"`, `"1e3"`, `"main"`, `"refs/pull/42/head"`, `"1234567890"`,
		`"` + head[:39] + `"`, `"` + strings.Repeat("g", 40) + `"`, `["42"]`} {
		eng := &sourceRefEngine{}
		_, err := methods["factory.pipeline_source"].call(eng, json.RawMessage(`{"factory":"f","repo":"a/b","ref":`+raw+`}`))
		if wireCode(t, err) != CodeBadParams || len(eng.sources) != 0 {
			t.Errorf("%s: %v after %d calls", raw, err, len(eng.sources))
		}
	}
}

func TestFactoryPipelineSourceRefSchema(t *testing.T) {
	defs := Schema()["$defs"].(map[string]any)
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["pipeline_source_ref"] == nil {
		t.Fatal("capabilities do not carry pipeline_source_ref")
	}
	src := defs["FactoryPipelineSource"].(map[string]any)["properties"].(map[string]any)
	if src["ref"] == nil || src["pr"] == nil {
		t.Fatalf("the source does not carry ref and pr: %v", src)
	}
}
