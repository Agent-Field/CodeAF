package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// pipelinePREngine is a factory engine that is also a FactoryPipelineOpener and
// keeps what each call was handed.
type pipelinePREngine struct {
	factoryEngine
	calls []FactoryPipelinePRParams
	err   error
}

func (r *pipelinePREngine) FactoryPipelinePR(_ context.Context, p FactoryPipelinePRParams) (FactoryPipelinePRDone, error) {
	r.calls = append(r.calls, p)
	if r.err != nil {
		return FactoryPipelinePRDone{}, r.err
	}
	return FactoryPipelinePRDone{Factory: p.Factory, Repo: p.Repo, Outcome: PipelinePROutcomeUnchanged, BaseSHA: strings.Repeat("a", 40)}, nil
}

const prGoodParams = `{"factory":"f","repo":"a/b","text":"pipeline: {}\n"}`

// ABSENT WITHOUT THE INTERFACE, AND NOT IMPLIED BY ANY OTHER FACTORY FEATURE.
func TestFactoryPipelinePRAbsentWithoutTheInterface(t *testing.T) {
	if len(factoryPipelinePRMethods) != 1 || factoryPipelinePRMethods["factory.pipeline_pr"].call == nil {
		t.Fatalf("pipeline methods: %v", factoryPipelinePRMethods)
	}
	raw := json.RawMessage(prGoodParams)
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &adminEngine{}, &linkEngine{}, &repoEngine{}} {
		if _, err := methods["factory.pipeline_pr"].call(e, raw); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if got := withFactoryPipelinePRFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryPipelinePRFeature(Hello{}, &pipelinePREngine{})
	if len(got.Features) != 1 || got.Features[0] != "factory.pipeline_pr" {
		t.Fatalf("a pipeline engine announced %v", got.Features)
	}
	e := &pipelinePREngine{}
	if h := withFactoryReposManageFeature(withFactoryLinksFeature(withFactoryAdminFeature(Hello{}, e), e), e); len(h.Features) != 0 {
		t.Fatalf("opening a pull request implied another feature: %v", h.Features)
	}
	for _, other := range []string{FeatureFactoryAdmin, FeatureFactoryLinks, FeatureFactoryReposManage} {
		if FeatureFactoryPipelinePR == other {
			t.Fatal("the feature is not distinct")
		}
	}
}

// PARAMS ARE EXACTLY THE ROUTE'S FIELDS PLUS factory AND repo: a path, verb,
// token, reviewer or anything else is bad_params, and the engine is not asked.
func TestFactoryPipelinePRParamsAreStrict(t *testing.T) {
	sha := strings.Repeat("a", 40)
	bad := []string{
		`{}`, `[]`, `null`, `"x"`,
		`{"factory":"f","repo":"a/b"}`, `{"factory":"f","text":"x"}`, `{"repo":"a/b","text":"x"}`,
		`{"factory":" ","repo":"a/b","text":"x"}`, `{"factory":"f","repo":"a/b","text":"   \n"}`, `{"factory":"f","repo":"a/b","text":""}`,
		`{"factory":"f","repo":"a/b","text":null,"title":"t"}`, `{"factory":"f","repo":"a/b","text":1}`, `{"factory":"f","repo":"a/b","text":["x"]}`,
		`{"factory":"f","repo":"a/b","text":"x","reviewers":["mona"]}`, `{"factory":"f","repo":"a/b","text":"x","token":"ghp_x"}`,
		`{"factory":"f","repo":"a/b","text":"x","github_token":"ghp_x"}`, `{"factory":"f","repo":"a/b","text":"x","path":"/v1/repos"}`,
		`{"factory":"f","repo":"a/b","text":"x","verb":"PUT"}`, `{"factory":"f","repo":"a/b","text":"x","url":"https://x"}`,
		`{"factory":"f","repo":"a/b","text":"x","edit":{}}`, `{"factory":"f","repo":"a/b","text":"x","draft":true}`, `{"factory":"f","repo":"a/b","text":"x","base":"main"}`,
		`{"factory":"f","repo":"a/b","text":"x","pull_request":1}`, `{"factory":"f","repo":"a/b","text":"x","delete_branch":true}`,
		`{"factory":"f","repo":"a/b","text":"x"} {"x":1}`,
		`{"factory":"f","repo":"","text":"x"}`, `{"factory":"f","repo":"ab","text":"x"}`, `{"factory":"f","repo":"a/b/c","text":"x"}`,
		`{"factory":"f","repo":"../b","text":"x"}`, `{"factory":"f","repo":"a/..","text":"x"}`, `{"factory":"f","repo":"a/b?x=1","text":"x"}`,
		`{"factory":"f","repo":"a/b#x","text":"x"}`, `{"factory":"f","repo":"a/é","text":"x"}`, `{"factory":"f","repo":"a/b\u0000","text":"x"}`,
		`{"factory":"f","repo":"a/b c","text":"x"}`, `{"factory":"f","repo":"a/%2e%2e","text":"x"}`, `{"factory":"f","repo":"a\\b/c","text":"x"}`,
		`{"factory":"f","repo":"` + strings.Repeat("o", RepoOwnerMax+1) + `/b","text":"x"}`,
		`{"factory":"f","repo":"a/` + strings.Repeat("n", RepoNameMax+1) + `","text":"x"}`,
		`{"factory":"f","repo":"a/b","text":"` + strings.Repeat("x", PipelinePRTextMax+1) + `"}`,
		`{"factory":"f","repo":"a/b","text":"x","title":"` + strings.Repeat("t", PipelinePRTitleMax+1) + `"}`,
		`{"factory":"f","repo":"a/b","text":"x","title":"one\ntwo"}`, `{"factory":"f","repo":"a/b","text":"x","title":"one\rtwo"}`, `{"factory":"f","repo":"a/b","text":"x","title":"nul\u0000"}`,
		`{"factory":"f","repo":"a/b","text":"x","body":"` + strings.Repeat("b", PipelinePRBodyMax+1) + `"}`, `{"factory":"f","repo":"a/b","text":"x","body":"nul\u0000"}`,
		`{"factory":"f","repo":"a/b","text":"x","branch":"-x"}`, `{"factory":"f","repo":"a/b","text":"x","branch":"a b"}`, `{"factory":"f","repo":"a/b","text":"x","branch":"HEAD"}`,
		`{"factory":"f","repo":"a/b","text":"x","branch":"a..b"}`, `{"factory":"f","repo":"a/b","text":"x","branch":"a/"}`, `{"factory":"f","repo":"a/b","text":"x","branch":"x.lock"}`,
		`{"factory":"f","repo":"a/b","text":"x","branch":"` + strings.Repeat("b", PipelinePRBranchMax+1) + `"}`,
		`{"factory":"f","repo":"a/b","text":"x","branch":" x"}`,
		`{"factory":"f","repo":"a/b","text":"x","expected_base_sha":"abc"}`, `{"factory":"f","repo":"a/b","text":"x","expected_base_sha":"` + strings.Repeat("g", 40) + `"}`,
		`{"factory":"f","repo":"a/b","text":"x","expected_base_sha":"` + sha + `0"}`, `{"factory":"f","repo":"a/b","text":"x","expected_base_sha":"main"}`,
	}
	eng := &pipelinePREngine{}
	for _, raw := range bad {
		_, err := methods["factory.pipeline_pr"].call(eng, json.RawMessage(raw))
		if err == nil {
			t.Errorf("accepted %.120s", raw)
		} else if wireCode(t, err) != CodeBadParams {
			t.Errorf("%.120s: %v", raw, err)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a bad request reached the engine: %d", len(eng.calls))
	}
}

func TestFactoryPipelinePRGoodParamsArePassedOnceAndExactly(t *testing.T) {
	eng := &pipelinePREngine{}
	upper := strings.ToUpper(strings.Repeat("ab", 20))
	text := "  pipeline: {}\n\n"
	raw, _ := json.Marshal(map[string]string{"factory": " f ", "repo": " Acme/api-2.x_y ", "text": text, "title": " A title ", "body": "why\n\nbecause", "branch": "codeaf/pipeline-x", "expected_base_sha": " " + upper + " "})
	out, err := methods["factory.pipeline_pr"].call(eng, raw)
	if err != nil || len(eng.calls) != 1 {
		t.Fatalf("%v after %d calls", err, len(eng.calls))
	}
	want := FactoryPipelinePRParams{Factory: "f", Repo: "Acme/api-2.x_y", Text: text, Title: " A title ", Body: "why\n\nbecause", Branch: "codeaf/pipeline-x", ExpectedBaseSHA: strings.ToLower(upper)}
	if eng.calls[0] != want {
		t.Fatalf("got %+v\nwant %+v", eng.calls[0], want)
	}
	data, _ := json.Marshal(out)
	var keys map[string]any
	_ = json.Unmarshal(data, &keys)
	if keys["factory"] != "f" || keys["repo"] != "Acme/api-2.x_y" || keys["outcome"] != "unchanged" || keys["pr"] != nil {
		t.Fatalf("receipt = %s", data)
	}
	// Only the three required fields are needed, and the biggest allowed ones fit.
	max := FactoryPipelinePRParams{Factory: "f", Repo: strings.Repeat("o", RepoOwnerMax) + "/" + strings.Repeat("n", RepoNameMax),
		Text: strings.Repeat("x", PipelinePRTextMax), Title: strings.Repeat("é", PipelinePRTitleMax), Body: strings.Repeat("b", PipelinePRBodyMax),
		Branch: strings.Repeat("b", PipelinePRBranchMax)}
	if _, err := CheckPipelinePR(max); err != nil {
		t.Fatal(err)
	}
	if _, err := methods["factory.pipeline_pr"].call(eng, json.RawMessage(prGoodParams)); err != nil || len(eng.calls) != 2 {
		t.Fatalf("%v after %d calls", err, len(eng.calls))
	}
}

// An engine's refusal is passed on whole, with its Detail, and asked once.
func TestFactoryPipelinePRPassesTheEnginesErrorAndAsksOnce(t *testing.T) {
	for _, code := range []string{CodeUncertain, CodeConflict, CodeForbidden, CodeSignedOut, CodeGitHubExpired, CodeBusy, CodeUnreachable, CodeNotFound, CodeBadParams, CodeEngine} {
		e := Errorf(code, "words for %s", code)
		e.Detail = FactoryPipelinePRFailure{Factory: "f", Repo: "a/b", Outcome: PipelinePROutcomeUnknown, Stage: "commit", Branch: "x", BranchLeft: true}
		eng := &pipelinePREngine{err: e}
		_, err := methods["factory.pipeline_pr"].call(eng, json.RawMessage(prGoodParams))
		if wireCode(t, err) != code || len(eng.calls) != 1 {
			t.Errorf("%s: %v after %d calls", code, err, len(eng.calls))
		}
		got, _ := json.Marshal(err)
		if !strings.Contains(string(got), `"branch_left":true`) || !strings.Contains(string(got), `"outcome":"unknown"`) {
			t.Errorf("%s: the typed detail did not travel: %s", code, got)
		}
	}
}

// THE RESULT AND THE DETAIL CARRY ONLY WHAT THE ROUTE DEFINES, and the schema
// names the method, the capability and the feature.
func TestFactoryPipelinePRSchemaIsTheRoutesFieldsAndNothingElse(t *testing.T) {
	if got := fieldNames(reflect.TypeFor[FactoryPipelinePRParams]()); !reflect.DeepEqual(got,
		[]string{"factory", "repo", "text", "title", "body", "branch", "expected_base_sha"}) {
		t.Fatalf("param fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryPipelinePRDone]()); !reflect.DeepEqual(got, []string{"factory", "repo", "outcome", "pr", "base_sha"}) {
		t.Fatalf("receipt fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryPipelinePR]()); !reflect.DeepEqual(got, []string{"number", "url", "branch", "base", "base_sha", "head_sha"}) {
		t.Fatalf("pull request fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryPipelinePRFailure]()); !reflect.DeepEqual(got,
		[]string{"factory", "repo", "outcome", "stage", "github_status", "branch", "branch_left", "request_id"}) {
		t.Fatalf("failure fields: %v", got)
	}
	doc := Schema()["methods"].(map[string]any)
	entry, ok := doc["factory.pipeline_pr"].(map[string]any)
	if !ok || entry["params"] == nil || entry["result"] == nil {
		t.Fatal("schema has no entry for factory.pipeline_pr")
	}
	defs := Schema()["$defs"].(map[string]any)
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["pipeline_pr"] == nil {
		t.Fatal("capabilities do not carry pipeline_pr")
	}
}
