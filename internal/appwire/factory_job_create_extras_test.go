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
		"FactoryCapabilities":           {"create_issue", "create_group", "create_budget", "job_context", "create_key"},
		"in.FactoryJobCreateParams":     {"open_issue", "group", "milestone", "budget", "key"},
		"in.FactoryJobContextSetParams": {"factory", "job", "expect", "context"},
		"in.FactoryJobContext":          {"title", "markdown", "source"},
	} {
		for _, field := range fields {
			if _, ok := schema.Defs[def].Properties[field]; !ok {
				t.Errorf("%s lacks %s", def, field)
			}
		}
	}
	if _, ok := schema.Defs["in.FactoryJobCreateParams"].Properties["context"]; ok {
		t.Fatal("create schema still carries context")
	}
	if _, ok := schema.Defs["FactoryCapabilities"].Properties["create_context"]; ok {
		t.Fatal("capabilities still advertise create_context")
	}
}

// E5/E6/E10/B1: valid extras reach the actor, including the open_issue wire flag.
func TestCreateExtrasCarryTrimmedArguments(t *testing.T) {
	for _, budget := range FactoryBudgetSteps {
		eng := &actorEngine{}
		params := FactoryJobCreateParams{
			Factory: "f", Repo: "o/r", Title: "Fix", Text: strings.Repeat("é", MaxFactoryJobText),
			OpenIssue: true, Group: "  batch  ", Milestone: "  release  ", Budget: &budget,
			Key: strings.Repeat("k", MaxFactoryJobKey),
		}
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := methods["factory.job_create"].call(eng, raw); err != nil {
			t.Fatal(err)
		}
		got := eng.got[0].(FactoryJobCreateParams)
		if got.Group != "batch" || got.Milestone != "release" || !got.OpenIssue || *got.Budget != budget {
			t.Fatalf("create arguments: %+v", got)
		}
		if got.Text != params.Text || got.Key != params.Key {
			t.Fatal("literal text or key changed")
		}
	}
}

// E5/E6/E10: bad extras name their field and limit before reaching the actor.
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

// E5/E10: inclusive label and key-alphabet boundaries are accepted.
func TestCreateExtrasInclusiveBounds(t *testing.T) {
	eng := &actorEngine{}
	params := FactoryJobCreateParams{
		Factory: "f", Repo: "o/r", Title: "Fix", Group: strings.Repeat("é", MaxFactoryJobLabel),
		Milestone: strings.Repeat("é", MaxFactoryJobLabel), Key: "AZaz09._:-",
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

// E15: create refuses context and the old issue wire name as unknown fields.
func TestCreateExtrasRefuseRemovedFields(t *testing.T) {
	for _, field := range []string{`"context":[]`, `"issue":true`} {
		eng := &actorEngine{}
		_, err := methods["factory.job_create"].call(eng, json.RawMessage(`{"factory":"f","repo":"o/r","title":"Fix",`+field+`}`))
		if wireCode(t, err) != CodeBadParams || len(eng.acts) != 0 {
			t.Fatalf("removed create field: %v", err)
		}
	}
}
