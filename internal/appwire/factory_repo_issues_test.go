package appwire

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

type repoIssuesEngine struct {
	factoryEngine
	got []FactoryRepoIssuesParams
}

func (e *repoIssuesEngine) FactoryRepoIssues(_ context.Context, p FactoryRepoIssuesParams) (FactoryRepoIssues, error) {
	e.got = append(e.got, p)
	return FactoryRepoIssues{Factory: p.Factory}, nil
}

type onlyRepoIssuesReader struct{ fakeEngine }

func (*onlyRepoIssuesReader) FactoryRepoIssues(context.Context, FactoryRepoIssuesParams) (FactoryRepoIssues, error) {
	return FactoryRepoIssues{}, nil
}

// C1: Only a Factory with this reader announces and serves the method.
func TestRepoIssuesNeedsItsFactoryReader(t *testing.T) {
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &onlyRepoIssuesReader{}} {
		_, err := methods[FeatureFactoryRepoIssues].call(e, json.RawMessage(`{"factory":"f","repo":"acme/api"}`))
		if wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if h := withFactoryRepoIssuesFeature(Hello{}, e); len(h.Features) != 0 {
			t.Errorf("%T announced %v", e, h.Features)
		}
	}
	h := withFactoryRepoIssuesFeature(Hello{}, &repoIssuesEngine{})
	if len(h.Features) != 1 || h.Features[0] != FeatureFactoryRepoIssues {
		t.Fatalf("hello = %+v", h)
	}
}

// C4: Defaults and valid cursors reach the reader without changing since.
func TestRepoIssuesChecksAndDefaultsItsParams(t *testing.T) {
	e := &repoIssuesEngine{}
	for _, raw := range []string{
		`{"factory":" f ","repo":" acme/api "}`,
		`{"factory":"f","repo":"acme/api","since":"2026-11-01T02:03:04.123+05:30","page":10}`,
	} {
		if _, err := methods[FeatureFactoryRepoIssues].call(e, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.got) != 2 || e.got[0].Factory != "f" || e.got[0].Repo != "acme/api" || *e.got[0].Page != 1 ||
		*e.got[1].Page != 10 || e.got[1].Since != "2026-11-01T02:03:04.123+05:30" {
		t.Fatalf("reader got %+v", e.got)
	}
}

// C4: Invalid input is refused before the reader can send a request.
func TestRepoIssuesRefusesBadParamsBeforeReading(t *testing.T) {
	e := &repoIssuesEngine{}
	for _, raw := range []string{
		`{}`, `{"factory":"f"}`, `{"repo":"acme/api"}`,
		`{"factory":"f","repo":"acme"}`, `{"factory":"f","repo":"acme/../api"}`,
		`{"factory":"f","repo":"acme/api?x=1"}`,
		`{"factory":"f","repo":"acme/api","since":"yesterday"}`,
		`{"factory":"f","repo":"acme/api","since":"2026-11-01"}`,
		`{"factory":"f","repo":"acme/api","page":0}`,
		`{"factory":"f","repo":"acme/api","page":-1}`,
		`{"factory":"f","repo":"acme/api","page":11}`,
		`{"factory":"f","repo":"acme/api","page":1.5}`,
		`{"factory":"f","repo":"acme/api","page":"2"}`,
		`{"factory":"f","repo":"acme/api","state":"open"}`,
	} {
		if _, err := methods[FeatureFactoryRepoIssues].call(e, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", raw, err)
		}
	}
	if len(e.got) != 0 {
		t.Fatalf("refused input reached the reader: %+v", e.got)
	}
}

// C1, C2, C3, C4: The independently registered method publishes its params,
// result, issue states, timestamps, optional fields and literal capability key.
func TestFactoryRepoIssuesSchemaNamesTheMethodAndTheCapability(t *testing.T) {
	doc := Schema()
	wantMethod := map[string]any{
		"params": map[string]any{"$ref": "#/$defs/in.FactoryRepoIssuesParams"},
		"result": map[string]any{"$ref": "#/$defs/FactoryRepoIssues"},
	}
	if got := doc["methods"].(map[string]any)["factory.repo_issues"]; !reflect.DeepEqual(got, wantMethod) {
		t.Fatalf("method schema = %#v, want %#v", got, wantMethod)
	}
	defs := doc["$defs"].(map[string]any)
	text := map[string]any{"type": "string"}
	integer := map[string]any{"type": "integer"}
	stamp := map[string]any{"type": "string", "format": "date-time"}
	null := map[string]any{"type": "null"}
	for _, tc := range []struct {
		name     string
		fields   map[string]any
		required []string
	}{
		{"in.FactoryRepoIssuesParams", map[string]any{
			"factory": text, "repo": text, "since": text,
			"page": map[string]any{"anyOf": []any{integer, null}},
		}, nil},
		{"FactoryRepoIssues", map[string]any{
			"factory": text, "repo": text, "rows": integer,
			"issues": map[string]any{"anyOf": []any{
				map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/RepoIssue"}}, null,
			}},
			"newest": map[string]any{"anyOf": []any{stamp, null}},
		}, []string{"factory", "repo", "issues", "rows"}},
		{"RepoIssue", map[string]any{
			"id": integer, "number": integer, "title": text, "body": text, "url": text,
			"state":      map[string]any{"type": "string", "enum": []string{"open", "closed"}},
			"created_at": stamp, "updated_at": stamp,
			"body_cut": map[string]any{"type": "boolean"},
		}, []string{"id", "number", "title", "body", "url", "state", "created_at", "updated_at"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := map[string]any{"type": "object", "properties": tc.fields, "additionalProperties": false}
			if tc.required != nil {
				want["required"] = tc.required
			}
			if got := defs[tc.name]; !reflect.DeepEqual(got, want) {
				t.Fatalf("schema = %#v, want %#v", got, want)
			}
		})
	}
	caps := defs["FactoryCapabilities"].(map[string]any)["properties"].(map[string]any)
	if got := caps["repo_issues"]; !reflect.DeepEqual(got, map[string]any{"type": "boolean"}) {
		t.Fatalf("repo_issues capability schema = %#v", got)
	}
}
