package appwire

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// AN AGENT'S UNKNOWN TIMES STAY OFF THE WIRE. A conversation the person never
// spoke in has no last_active_at, and an agent whose times are unknown says
// neither, rather than 0001-01-01T00:00:00Z, which a window draws as a date in
// the first century; a known time is said as it is. The schema agrees: neither
// field is required of an agent.
func TestAnAgentsUnknownTimesStayOffTheWire(t *testing.T) {
	raw, err := json.Marshal(Agent{ID: "a", Title: "Take the findings and…", Workspace: "/w", State: StateIdle})
	if err != nil {
		t.Fatal(err)
	}
	for _, said := range []string{"updated_at", "last_active_at", "0001-01-01"} {
		if strings.Contains(string(raw), said) {
			t.Fatalf("an agent with no known times says %q: %s", said, raw)
		}
	}
	at := time.Date(2026, 10, 15, 19, 30, 0, 0, time.UTC)
	raw, err = json.Marshal(Agent{ID: "a", State: StateIdle, UpdatedAt: at, LastActiveAt: at})
	if err != nil {
		t.Fatal(err)
	}
	for _, said := range []string{`"updated_at":"2026-10-15T19:30:00Z"`, `"last_active_at":"2026-10-15T19:30:00Z"`} {
		if !strings.Contains(string(raw), said) {
			t.Fatalf("a known time is not said as %s: %s", said, raw)
		}
	}

	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	agent := doc.Defs["Agent"]
	for _, field := range []string{"updated_at", "last_active_at"} {
		if agent.Properties[field] == nil {
			t.Errorf("the schema's Agent lost %s", field)
		}
		if slices.Contains(agent.Required, field) {
			t.Errorf("the schema requires %s of every agent", field)
		}
	}
}
