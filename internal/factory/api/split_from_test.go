package api

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// S2/S4: lineage survives the job codec and is absent from ordinary job JSON.
func TestSplitFromJobCodecAndOmission(t *testing.T) {
	for _, parent := range []int{0, 80} {
		job := FromItem(factory.Item{ID: 81, Kind: factory.KindIssue, State: factory.StateNew, SplitFrom: parent})
		raw, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if value, ok := fields["split_from"]; ok != (parent > 0) || ok && string(value) != "80" {
			t.Fatalf("split_from JSON = %s (present=%v), parent=%d", value, ok, parent)
		}
		var decoded Job
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if got := ToItem(decoded); got.SplitFrom != parent || got.FromJob != 0 {
			t.Fatalf("split lineage after round trip: split_from=%d from_job=%d", got.SplitFrom, got.FromJob)
		}
	}
}
