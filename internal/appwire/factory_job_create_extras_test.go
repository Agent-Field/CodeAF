package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// E1: the generated schema exposes every create capability and parameter.
func TestCreateExtrasSchema(t *testing.T) {
	raw, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for def, fields := range map[string][]string{
		"FactoryCapabilities":       {"create_issue", "create_group", "create_budget", "create_context", "create_key"},
		"in.FactoryJobCreateParams": {"open_issue", "group", "milestone", "budget", "context", "key"},
		"in.FactoryJobContext":      {"title", "markdown", "source"},
	} {
		for _, field := range fields {
			if _, ok := schema.Defs[def].Properties[field]; !ok {
				t.Errorf("%s lacks %s", def, field)
			}
		}
	}
}

// E5/E6/E7/E8/E10: valid extras reach the actor whole, with independent text limits.
func TestCreateExtrasCarryTrimmedArguments(t *testing.T) {
	for _, budget := range FactoryBudgetSteps {
		eng := &actorEngine{}
		params := FactoryJobCreateParams{
			Factory: "f", Repo: "o/r", Title: "Fix", Text: strings.Repeat("é", MaxFactoryJobText),
			OpenIssue: true, Group: "  batch  ", Milestone: "  release  ", Budget: &budget,
			Context: []FactoryJobContext{{Title: "  reference  ", Markdown: strings.Repeat("é", MaxFactoryJobContext), Source: strings.Repeat("é", MaxFactoryJobContextSource)}},
			Key:     strings.Repeat("k", MaxFactoryJobKey),
		}
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := methods["factory.job_create"].call(eng, raw); err != nil {
			t.Fatal(err)
		}
		got := eng.got[0].(FactoryJobCreateParams)
		if got.Group != "batch" || got.Milestone != "release" || got.Context[0].Title != "reference" || !got.Issue || *got.Budget != budget {
			t.Fatalf("create arguments: %+v", got)
		}
		if got.Text != params.Text || got.Context[0].Markdown != params.Context[0].Markdown || got.Key != params.Key {
			t.Fatal("literal text, context or key changed")
		}
	}
}

// E5/E6/E8/E10: bad extras name their field and limit before reaching the actor.
func TestCreateExtrasRefuseBeforeActor(t *testing.T) {
	cases := []struct {
		name, field, limit string
		value              any
	}{
		{"group length", "group", "200", strings.Repeat("g", 201)},
		{"group control", "group", "control", "batch\x00"},
		{"milestone length", "milestone", "200", strings.Repeat("m", 201)},
		{"milestone control", "milestone", "control", "release\nname"},
		{"budget", "budget", "5 10 20 30", 7},
		{"zero budget", "budget", "5 10 20 30", 0},
		{"key alphabet", "key", "128", "a b"},
		{"key length", "key", "128", strings.Repeat("k", 129)},
		{"context total", "context", "100000", []FactoryJobContext{{Title: "a", Markdown: strings.Repeat("x", 50000)}, {Title: "b", Markdown: strings.Repeat("x", 50001)}}},
		{"context count", "context", "20", make([]FactoryJobContext, 21)},
		{"context title empty", "context", "title", []FactoryJobContext{{Title: "  ", Markdown: "x"}}},
		{"context title length", "context", "200", []FactoryJobContext{{Title: strings.Repeat("t", 201), Markdown: "x"}}},
		{"context source length", "context", "500", []FactoryJobContext{{Title: "t", Markdown: "x", Source: strings.Repeat("s", 501)}}},
		{"context markdown empty", "context", "markdown", []FactoryJobContext{{Title: "t"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := &actorEngine{}
			raw, err := json.Marshal(map[string]any{"factory": "f", "repo": "o/r", "title": "Fix", tc.field: tc.value})
			if err != nil {
				t.Fatal(err)
			}
			_, err = methods["factory.job_create"].call(eng, raw)
			if wireCode(t, err) != CodeBadParams || !strings.Contains(err.Error(), tc.field) || !strings.Contains(err.Error(), tc.limit) {
				t.Fatalf("refusal: %v", err)
			}
			if len(eng.acts) != 0 {
				t.Fatal("invalid extras reached the actor")
			}
		})
	}
}

// E5/E7/E8/E10: inclusive character, count and key-alphabet boundaries are accepted.
func TestCreateExtrasInclusiveBounds(t *testing.T) {
	eng := &actorEngine{}
	params := FactoryJobCreateParams{
		Factory: "f", Repo: "o/r", Title: "Fix", Group: strings.Repeat("é", MaxFactoryJobLabel),
		Milestone: strings.Repeat("é", MaxFactoryJobLabel), Key: "AZaz09._:-",
	}
	for i := 0; i < MaxFactoryJobContextItems; i++ {
		params.Context = append(params.Context, FactoryJobContext{Title: strings.Repeat("é", MaxFactoryJobContextTitle), Markdown: "reference"})
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := methods["factory.job_create"].call(eng, raw); err != nil {
		t.Fatal(err)
	}
	if len(eng.acts) != 1 {
		t.Fatal("valid bounds did not reach the actor")
	}
}

// E8: malformed attachment fields are refused by the strict wire decoder.
func TestCreateExtrasMalformedContext(t *testing.T) {
	for _, context := range []string{
		`[{"title":"reference","markdown":7}]`,
		`[{"title":7,"markdown":"text"}]`,
		`[{"title":"reference","markdown":"text","source":7}]`,
		`[{"title":"reference","markdown":"text","unknown":true}]`,
		`"reference"`,
	} {
		eng := &actorEngine{}
		_, err := methods["factory.job_create"].call(eng, json.RawMessage(`{"factory":"f","repo":"o/r","title":"Fix","context":`+context+`}`))
		if wireCode(t, err) != CodeBadParams || len(eng.acts) != 0 {
			t.Fatalf("malformed context: %v", err)
		}
	}
}
