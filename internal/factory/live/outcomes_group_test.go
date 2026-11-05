package live_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// An outcome carries the group its job was made with, and none when the job has none.
func TestOutcomeCarriesTheJobsGroup(t *testing.T) {
	for _, group := range []string{"batch-7", ""} {
		before := factory.Item{ID: 3, State: factory.StateRunning, Group: group}
		after := factory.Item{ID: 3, Title: "Fix", State: factory.StateShipped, Group: group}
		events := live.Outcomes(live.OutcomeBasis(before), after, time.Now())
		if len(events) != 1 {
			t.Fatalf("group %q: %d events", group, len(events))
		}
		var got live.Outcome
		if err := json.Unmarshal(events[0].Data, &got); err != nil || got.Group != group {
			t.Fatalf("group %q: outcome %+v, %v", group, got, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(events[0].Data, &raw); err != nil {
			t.Fatal(err)
		}
		if _, present := raw["group"]; present != (group != "") {
			t.Fatalf("group %q: wire group present=%v", group, present)
		}
	}
}
