package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// commentEngine is a factory engine that is also a FactoryIssueCommenter and
// keeps what each call was handed.
type commentEngine struct {
	factoryEngine
	calls []FactoryIssueCommentParams
	err   error
}

func (r *commentEngine) FactoryIssueComment(_ context.Context, p FactoryIssueCommentParams) (FactoryIssueCommentDone, error) {
	r.calls = append(r.calls, p)
	if r.err != nil {
		return FactoryIssueCommentDone{}, r.err
	}
	return FactoryIssueCommentDone{Factory: p.Factory, Repo: p.Repo, Number: p.Number,
		Comment: FactoryIssueComment{ID: 9001, URL: "https://github.com/a/b/issues/7#issuecomment-9001", CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}}, nil
}

const commentGoodParams = `{"factory":"f","repo":"a/b","number":7,"body":"Which browser?"}`

func TestFactoryIssueCommentAbsentWithoutTheInterface(t *testing.T) {
	raw := json.RawMessage(commentGoodParams)
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &pipelinePREngine{}, &repoEngine{}} {
		if _, err := methods["factory.issue_comment"].call(e, raw); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if got := withFactoryIssueCommentFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryIssueCommentFeature(Hello{}, &commentEngine{})
	if len(got.Features) != 1 || got.Features[0] != "factory.issue_comment" || FeatureFactoryIssueComment != "factory.issue_comment" {
		t.Fatalf("a commenting engine announced %v", got.Features)
	}
	if h := withFactoryPipelinePRFeature(Hello{}, &commentEngine{}); len(h.Features) != 0 {
		t.Fatalf("posting a comment implied another feature: %v", h.Features)
	}
}

func TestFactoryIssueCommentParamsAreStrict(t *testing.T) {
	bad := []string{
		`{}`, `[]`, `null`, `"x"`, `{"factory":"f","repo":"a/b","number":7,"body":"x"} {"x":1}`,
		`{"repo":"a/b","number":7,"body":"x"}`, `{"factory":" ","repo":"a/b","number":7,"body":"x"}`,
		`{"factory":"f","number":7,"body":"x"}`, `{"factory":"f","repo":"ab","number":7,"body":"x"}`, `{"factory":"f","repo":"a/b/c","number":7,"body":"x"}`,
		`{"factory":"f","repo":"../b","number":7,"body":"x"}`, `{"factory":"f","repo":"a/b?x","number":7,"body":"x"}`,
		`{"factory":"f","repo":"a/b","body":"x"}`, `{"factory":"f","repo":"a/b","number":0,"body":"x"}`, `{"factory":"f","repo":"a/b","number":-3,"body":"x"}`,
		`{"factory":"f","repo":"a/b","number":7.5,"body":"x"}`, `{"factory":"f","repo":"a/b","number":"7","body":"x"}`,
		`{"factory":"f","repo":"a/b","number":7}`, `{"factory":"f","repo":"a/b","number":7,"body":""}`, `{"factory":"f","repo":"a/b","number":7,"body":" \n\t"}`,
		`{"factory":"f","repo":"a/b","number":7,"body":"nul\u0000"}`, `{"factory":"f","repo":"a/b","number":7,"body":1}`,
		`{"factory":"f","repo":"a/b","number":7,"body":"` + strings.Repeat("x", IssueCommentBodyMax+1) + `"}`,
		`{"factory":"f","repo":"a/b","number":7,"body":"x","token":"ghp_x"}`, `{"factory":"f","repo":"a/b","number":7,"body":"x","path":"/v1"}`,
		`{"factory":"f","repo":"a/b","number":7,"body":"x","comment_id":3}`, `{"factory":"f","repo":"a/b","number":7,"body":"x","retry":true}`,
	}
	eng := &commentEngine{}
	for _, raw := range bad {
		if _, err := methods["factory.issue_comment"].call(eng, json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %.120s", raw)
		} else if wireCode(t, err) != CodeBadParams {
			t.Errorf("%.120s: %v", raw, err)
		}
	}
	// Invalid UTF-8 cannot travel in a JSON string, so it is checked directly.
	if _, err := CheckIssueComment(FactoryIssueCommentParams{Factory: "f", Repo: "a/b", Number: 7, Body: "bad \xff byte"}); wireCode(t, err) != CodeBadParams {
		t.Errorf("invalid UTF-8: %v", err)
	}
	if len(eng.calls) != 0 {
		t.Fatalf("a bad request reached the engine: %d", len(eng.calls))
	}
}

func TestFactoryIssueCommentGoodParamsArePassedOnceAndExactly(t *testing.T) {
	eng := &commentEngine{}
	raw, _ := json.Marshal(map[string]any{"factory": " f ", "repo": " Acme/api ", "number": 7, "body": "  Which browser?\n\nThanks.\n"})
	out, err := methods["factory.issue_comment"].call(eng, raw)
	if err != nil || len(eng.calls) != 1 {
		t.Fatalf("%v after %d calls", err, len(eng.calls))
	}
	want := FactoryIssueCommentParams{Factory: "f", Repo: "Acme/api", Number: 7, Body: "  Which browser?\n\nThanks.\n"}
	if eng.calls[0] != want {
		t.Fatalf("got %+v\nwant %+v", eng.calls[0], want)
	}
	data, _ := json.Marshal(out)
	const receipt = `{"factory":"f","repo":"Acme/api","number":7,"comment":{"id":9001,"url":"https://github.com/a/b/issues/7#issuecomment-9001","created_at":"2026-11-01T12:00:00Z"}}`
	if string(data) != receipt {
		t.Fatalf("receipt = %s", data)
	}
	if _, err := CheckIssueComment(FactoryIssueCommentParams{Factory: "f", Repo: "a/b", Number: 1, Body: strings.Repeat("é", IssueCommentBodyMax/2)}); err != nil {
		t.Fatal(err)
	}
}

func TestFactoryIssueCommentPassesTheEnginesErrorAndAsksOnce(t *testing.T) {
	for _, code := range []string{CodeUncertain, CodeForbidden, CodeGitHubExpired, CodeBusy, CodeNotFound, CodeBadParams, CodeEngine} {
		e := Errorf(code, "words for %s", code)
		e.Detail = FactoryIssueCommentFailure{Factory: "f", Repo: "a/b", Number: 7, Outcome: IssueCommentOutcomeUnknown, Stage: "comment", RequestID: "abcd"}
		eng := &commentEngine{err: e}
		_, err := methods["factory.issue_comment"].call(eng, json.RawMessage(commentGoodParams))
		if wireCode(t, err) != code || len(eng.calls) != 1 {
			t.Errorf("%s: %v after %d calls", code, err, len(eng.calls))
		}
		if got, _ := json.Marshal(err); !strings.Contains(string(got), `"outcome":"unknown"`) || !strings.Contains(string(got), `"stage":"comment"`) {
			t.Errorf("%s: the typed detail did not travel: %s", code, got)
		}
	}
}

func TestFactoryIssueCommentSchemaIsTheRoutesFieldsAndNothingElse(t *testing.T) {
	if got := fieldNames(reflect.TypeFor[FactoryIssueCommentParams]()); !reflect.DeepEqual(got, []string{"factory", "repo", "number", "body"}) {
		t.Fatalf("param fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryIssueCommentDone]()); !reflect.DeepEqual(got, []string{"factory", "repo", "number", "comment"}) {
		t.Fatalf("receipt fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryIssueComment]()); !reflect.DeepEqual(got, []string{"id", "url", "created_at"}) {
		t.Fatalf("comment fields: %v", got)
	}
	if got := fieldNames(reflect.TypeFor[FactoryIssueCommentFailure]()); !reflect.DeepEqual(got,
		[]string{"factory", "repo", "number", "outcome", "stage", "github_status", "request_id"}) {
		t.Fatalf("failure fields: %v", got)
	}
	doc := Schema()["methods"].(map[string]any)
	if entry, ok := doc["factory.issue_comment"].(map[string]any); !ok || entry["params"] == nil || entry["result"] == nil {
		t.Fatal("schema has no entry for factory.issue_comment")
	}
	caps := Schema()["$defs"].(map[string]any)["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if caps["issue_comment"] == nil {
		t.Fatal("capabilities do not carry issue_comment")
	}
}
