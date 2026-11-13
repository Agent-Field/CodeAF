package appwire

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

type contextEngine struct{ actorEngine }

func (e *contextEngine) FactoryJobContextSet(_ context.Context, p FactoryJobContextSetParams) (FactoryJobActed, error) {
	return e.did(JobActContextSet, p)
}

// E1: only engines carrying the separate context write announce its feature.
func TestJobContextFeature(t *testing.T) {
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &actorEngine{}} {
		if slices.Contains(withFactoryJobContextFeature(Hello{}, e).Features, FeatureFactoryJobContext) {
			t.Fatal("engine without context setter announced context")
		}
		if _, err := methods["factory.job_context_set"].call(e, json.RawMessage(`{}`)); wireCode(t, err) != CodeUnknownMethod {
			t.Fatal(err)
		}
	}
	if !slices.Contains(withFactoryJobContextFeature(Hello{}, &contextEngine{}).Features, "factory.job_context") {
		t.Fatal("context setter did not announce context")
	}
}

// E7'/E8': complete attachments and inclusive Unicode bounds survive the wire; [] clears.
func TestJobContextSetCarriesBoundsAndClear(t *testing.T) {
	items := []FactoryJobContext{{Title: " reference ", Markdown: strings.Repeat("é", MaxFactoryJobContext), Source: strings.Repeat("é", MaxFactoryJobContextSource)}}
	bounded := make([]FactoryJobContext, MaxFactoryJobContextItems)
	for i := range bounded {
		bounded[i] = FactoryJobContext{Title: strings.Repeat("é", MaxFactoryJobContextTitle), Markdown: "body"}
	}
	for _, attachments := range [][]FactoryJobContext{items, bounded, {}} {
		eng := &contextEngine{}
		raw, err := json.Marshal(map[string]any{"factory": " f ", "job": 1, "expect": map[string]int{"version": 3}, "context": attachments})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := methods["factory.job_context_set"].call(eng, raw); err != nil {
			t.Fatal(err)
		}
		got := eng.got[0].(FactoryJobContextSetParams)
		if got.Factory != "f" || got.Job != 1 || len(got.Context) != len(attachments) || eng.acts[0] != "context_set" {
			t.Fatalf("context arguments: %+v", got)
		}
		for i, c := range got.Context {
			if c.Title != strings.TrimSpace(attachments[i].Title) || c.Markdown != attachments[i].Markdown || c.Source != attachments[i].Source {
				t.Fatal("attachment changed")
			}
		}
	}
}

// E8': all context limits and malformed shapes fail before the engine.
func TestJobContextSetRefusesBeforeEngine(t *testing.T) {
	cases := []struct{ context, words string }{
		{`[{"title":"a","markdown":"` + strings.Repeat("x", 100001) + `"}]`, "100000"},
		{`[{"title":"a","markdown":"` + strings.Repeat("x", 50000) + `"},{"title":"b","markdown":"` + strings.Repeat("x", 50001) + `"}]`, "100000"},
		{`[` + strings.Repeat(`{"title":"a","markdown":"x"},`, 20) + `{"title":"a","markdown":"x"}]`, "20"},
		{`[{"title":"  ","markdown":"x"}]`, "title"},
		{`[{"title":"` + strings.Repeat("t", 201) + `","markdown":"x"}]`, "200"},
		{`[{"title":"t","markdown":"x","source":"` + strings.Repeat("s", 501) + `"}]`, "500"},
		{`[{"title":"t","markdown":"  "}]`, "markdown"},
		{`[{"title":"t","markdown":7}]`, "markdown"},
		{`[{"title":7,"markdown":"x"}]`, "title"},
		{`[{"title":"t","markdown":"x","source":7}]`, "source"},
		{`[{"title":"t","markdown":"x","unknown":true}]`, "unknown"},
		{`"reference"`, "context"},
		{`null`, "context"},
	}
	for _, tc := range cases {
		eng := &contextEngine{}
		_, err := methods["factory.job_context_set"].call(eng, json.RawMessage(`{"factory":"f","job":1,"expect":{"version":3},"context":`+tc.context+`}`))
		if wireCode(t, err) != CodeBadParams || !strings.Contains(err.Error(), tc.words) || len(eng.acts) != 0 {
			t.Fatalf("context refusal: %v", err)
		}
	}
	for _, raw := range []string{
		`{"factory":"f","job":1,"context":[]}`,
		`{"factory":"f","job":1,"expect":{},"context":[]}`,
		`{"factory":"f","job":1,"expect":{"version":0},"context":[]}`,
		`{"factory":"f","job":1,"expect":{"version":3}}`,
		`{"factory":"f","job":0,"expect":{"version":3},"context":[]}`,
	} {
		eng := &contextEngine{}
		_, err := methods["factory.job_context_set"].call(eng, json.RawMessage(raw))
		if wireCode(t, err) != CodeBadParams || len(eng.acts) != 0 {
			t.Fatalf("missing context or guard: %v", err)
		}
	}
}
