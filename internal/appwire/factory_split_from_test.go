package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// S1/S2: split lineage is described by the schema and carried to the actor.
func TestFactoryCreateSplitFromSchemaAndArguments(t *testing.T) {
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
	for def, field := range map[string]string{
		"FactoryCapabilities": "create_split_from", "in.FactoryJobCreateParams": "split_from",
	} {
		if _, ok := schema.Defs[def].Properties[field]; !ok {
			t.Errorf("%s lacks %s", def, field)
		}
	}
	for _, value := range []int{1, 80} {
		eng := &actorEngine{}
		params, err := json.Marshal(map[string]any{"factory": "f", "repo": "o/r", "title": "Fix", "split_from": value})
		if err != nil {
			t.Fatal(err)
		}
		_, err = methods["factory.job_create"].call(eng, params)
		if err != nil {
			t.Fatal(err)
		}
		got := eng.got[0].(FactoryJobCreateParams)
		if got.SplitFrom == nil || *got.SplitFrom != value {
			t.Fatalf("split_from not carried: %+v", got)
		}
	}
}

// S3: explicit zero, negative and non-integer values never reach the actor.
func TestFactoryCreateSplitFromRefusesInvalidNumbers(t *testing.T) {
	for _, value := range []string{"0", "-1", "1.5", `"80"`} {
		t.Run(value, func(t *testing.T) {
			eng := &actorEngine{}
			_, err := methods["factory.job_create"].call(eng, json.RawMessage(`{"factory":"f","repo":"o/r","title":"Fix","split_from":`+value+`}`))
			if wireCode(t, err) != CodeBadParams || !strings.Contains(err.Error(), "split_from") {
				t.Fatalf("invalid split_from refusal: %v", err)
			}
			if len(eng.acts) != 0 {
				t.Fatal("invalid split_from reached the actor")
			}
		})
	}
}
