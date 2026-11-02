package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// pipelinePRUpdateEngine also adds commits to an open pull request.
type pipelinePRUpdateEngine struct{ pipelinePREngine }

func (*pipelinePRUpdateEngine) PipelinePRUpdates() {}

// THE FEATURE IS ITS OWN WORD, announced only by an engine that opens pull
// requests AND says it updates them.
func TestFactoryPipelinePRUpdateFeatureNeedsTheMarker(t *testing.T) {
	if FeatureFactoryPipelinePRUpdate != "factory.pipeline_pr_update" {
		t.Fatalf("feature %q", FeatureFactoryPipelinePRUpdate)
	}
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &pipelinePREngine{}} {
		if got := withFactoryPipelinePRUpdateFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryPipelinePRUpdateFeature(Hello{}, &pipelinePRUpdateEngine{})
	if len(got.Features) != 1 || got.Features[0] != FeatureFactoryPipelinePRUpdate {
		t.Fatalf("an updating engine announced %v", got.Features)
	}
}

// AN ENGINE WITHOUT THE WORD refuses pr as it always did: bad_params, and the
// engine is never asked.
func TestFactoryPipelinePRUpdateIsRefusedByAnEngineWithoutIt(t *testing.T) {
	eng := &pipelinePREngine{}
	for _, raw := range []string{
		`{"factory":"f","repo":"a/b","text":"x","pr":7}`,
		`{"factory":"f","repo":"a/b","text":"x","expected_head_sha":"` + strings.Repeat("a", 40) + `"}`,
	} {
		_, err := methods["factory.pipeline_pr"].call(eng, json.RawMessage(raw))
		if wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("the engine was asked %d times", len(eng.calls))
	}
}

func TestFactoryPipelinePRUpdateParams(t *testing.T) {
	sha := strings.Repeat("a", 40)
	bad := []string{
		`{"factory":"f","repo":"a/b","text":"x","pr":0,"expected_head_sha":"` + sha + `"}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":-1}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":"7"}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":7.5}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":7,"branch":"codeaf/x"}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":7,"body":"why"}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":7,"expected_base_sha":"` + sha + `"}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":7,"expected_head_sha":"abc"}`,
		`{"factory":"f","repo":"a/b","text":"x","pr":7,"expected_head_sha":"` + strings.Repeat("g", 40) + `"}`,
	}
	eng := &pipelinePRUpdateEngine{}
	for _, raw := range bad {
		if _, err := methods["factory.pipeline_pr"].call(eng, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a bad request reached the engine: %d", len(eng.calls))
	}
	upper := strings.ToUpper(strings.Repeat("ab", 20))
	raw := `{"factory":"f","repo":"a/b","text":"x","title":"t","pr":7,"expected_head_sha":" ` + upper + ` "}`
	if _, err := methods["factory.pipeline_pr"].call(eng, json.RawMessage(raw)); err != nil || len(eng.calls) != 1 {
		t.Fatalf("%v after %d calls", err, len(eng.calls))
	}
	want := FactoryPipelinePRParams{Factory: "f", Repo: "a/b", Text: "x", Title: "t", PR: 7, ExpectedHeadSHA: strings.ToLower(upper)}
	if eng.calls[0] != want {
		t.Fatalf("got %+v\nwant %+v", eng.calls[0], want)
	}
}

func TestFactoryPipelinePRUpdateSchema(t *testing.T) {
	defs := Schema()["$defs"].(map[string]any)
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["pipeline_pr_update"] == nil {
		t.Fatal("capabilities do not carry pipeline_pr_update")
	}
	done := defs["FactoryPipelinePRDone"].(map[string]any)["properties"].(map[string]any)
	data, _ := json.Marshal(done["outcome"])
	if !strings.Contains(string(data), `"updated"`) {
		t.Fatalf("outcome enum %s", data)
	}
	stages := strings.Join(PipelinePRStages, " ")
	if !strings.Contains(" "+stages+" ", " head ") {
		t.Fatalf("stages %v", PipelinePRStages)
	}
}
