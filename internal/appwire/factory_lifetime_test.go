package appwire

import (
	"encoding/json"
	"testing"
)

func TestFactoryJobSchemaIncludesOptionalLifetimeCost(t *testing.T) {
	raw, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	job, ok := doc.Defs["Job"]
	if !ok || job.Properties["lifetime_cost"] == nil {
		t.Fatal("Job schema missing lifetime_cost")
	}
	for _, key := range job.Required {
		if key == "lifetime_cost" {
			t.Fatal("lifetime_cost must be optional for older factories")
		}
	}
}
