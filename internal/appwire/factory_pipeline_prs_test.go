package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// pipelinePRsEngine is a factory engine that lists open pipeline pull requests
// and keeps what each call was handed.
type pipelinePRsEngine struct {
	factoryEngine
	calls []FactoryPipelinePRsParams
}

func (r *pipelinePRsEngine) FactoryPipelinePRs(_ context.Context, p FactoryPipelinePRsParams) (FactoryPipelinePRs, error) {
	r.calls = append(r.calls, p)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	return FactoryPipelinePRs{Factory: p.Factory, Repo: p.Repo, Complete: true, CheckedAt: at, PRs: []FactoryPipelineOpenPR{{
		Number: 7, URL: "https://github.com/acme/api/pull/7", Title: "Pipeline: add proof", Branch: "codeaf/pipeline-0a1b2c3d4e5f",
		HeadSHA: "3333333333333333333333333333333333333333", Author: "mona", FactoryBranch: true, CreatedAt: at, UpdatedAt: at,
	}}}, nil
}

// THE FEATURE IS ANNOUNCED ONLY BY AN ENGINE THAT LISTS, and implies nothing else.
func TestFactoryPipelinePRsFeatureIsAnnouncedOnlyByAListingEngine(t *testing.T) {
	raw := json.RawMessage(`{"factory":"f","repo":"acme/api"}`)
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &pipelinePREngine{}} {
		if _, err := methods["factory.pipeline_prs"].call(e, raw); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if got := withFactoryPipelinePRsFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryPipelinePRsFeature(Hello{}, &pipelinePRsEngine{})
	if len(got.Features) != 1 || got.Features[0] != "factory.pipeline_prs" || FeatureFactoryPipelinePRs != "factory.pipeline_prs" {
		t.Fatalf("a listing engine announced %v", got.Features)
	}
	if h := withFactoryPipelinePRFeature(Hello{}, &pipelinePRsEngine{}); len(h.Features) != 0 {
		t.Fatalf("listing implied opening: %v", h.Features)
	}
}

// THE PARAMS ARE EXACTLY {factory, repo}; the answer is the engine's, once.
func TestFactoryPipelinePRsParamsAreStrictAndTheAnswerIsTheEngines(t *testing.T) {
	eng := &pipelinePRsEngine{}
	for _, bad := range []string{`{}`, `null`, `{"factory":"f"}`, `{"repo":"a/b"}`, `{"factory":"f","repo":"ab"}`,
		`{"factory":"f","repo":"a/b/c"}`, `{"factory":"f","repo":"../b"}`, `{"factory":"f","repo":"a/b","state":"closed"}`,
		`{"factory":"f","repo":"a/b","token":"ghp_x"}`, `{"factory":"f","repo":"a/b"} {}`} {
		if _, err := methods["factory.pipeline_prs"].call(eng, json.RawMessage(bad)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a bad request reached the engine: %d", len(eng.calls))
	}
	out, err := methods["factory.pipeline_prs"].call(eng, json.RawMessage(`{"factory":" f ","repo":" acme/api "}`))
	if err != nil || len(eng.calls) != 1 || eng.calls[0] != (FactoryPipelinePRsParams{Factory: "f", Repo: "acme/api"}) {
		t.Fatalf("%v %+v", err, eng.calls)
	}
	data, _ := json.Marshal(out)
	const want = `{"factory":"f","repo":"acme/api","prs":[{"number":7,"url":"https://github.com/acme/api/pull/7","title":"Pipeline: add proof",` +
		`"branch":"codeaf/pipeline-0a1b2c3d4e5f","head_sha":"3333333333333333333333333333333333333333","author":"mona","draft":false,` +
		`"factory_branch":true,"created_at":"2026-11-01T12:00:00Z","updated_at":"2026-11-01T12:00:00Z"}],"complete":true,"checked_at":"2026-11-01T12:00:00Z"}`
	if string(data) != want {
		t.Fatalf("answer\n got %s\nwant %s", data, want)
	}
}

func TestFactoryPipelinePRsSchemaNamesTheMethodAndTheCapability(t *testing.T) {
	if got := fieldNames(reflect.TypeFor[FactoryPipelinePRsParams]()); !reflect.DeepEqual(got, []string{"factory", "repo"}) {
		t.Fatalf("param fields: %v", got)
	}
	doc := Schema()["methods"].(map[string]any)
	if entry, ok := doc["factory.pipeline_prs"].(map[string]any); !ok || entry["params"] == nil || entry["result"] == nil {
		t.Fatal("schema has no entry for factory.pipeline_prs")
	}
	caps := Schema()["$defs"].(map[string]any)["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["pipeline_prs"] == nil {
		t.Fatal("capabilities do not carry pipeline_prs")
	}
}
