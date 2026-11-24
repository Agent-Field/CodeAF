package session

import (
	"encoding/json"
	"testing"
)

// TestTheAskNamesItsKindInsideItsOwnProperties pins that ws_task_update's ask
// takes a kind, and that the schema still parses with kind INSIDE the ask's
// properties: a kind beside them is a schema the provider refuses.
func TestTheAskNamesItsKindInsideItsOwnProperties(t *testing.T) {
	for _, tool := range WorkstreamWorkTools() {
		if tool.Name != WSTaskUpdateTool {
			continue
		}
		var schema struct {
			Properties struct {
				Ask struct {
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"ask"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("ws_task_update's schema does not parse: %v", err)
		}
		if _, ok := schema.Properties.Ask.Properties["kind"]; !ok {
			t.Fatalf("the ask's properties have no kind: %v", schema.Properties.Ask.Properties)
		}
		if _, ok := schema.Properties.Ask.Properties["options"]; !ok || len(schema.Properties.Ask.Required) != 2 {
			t.Fatalf("the ask lost its options or its required list: %+v", schema.Properties.Ask)
		}
		return
	}
	t.Fatal("ws_task_update is not on the belt")
}
