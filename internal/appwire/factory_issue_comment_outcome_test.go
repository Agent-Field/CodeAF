package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// A REFUSAL OF factory.issue_comment CAN SAY WHETHER ANYTHING WAS POSTED: the
// typed detail every factory.job_* refusal carries has an optional outcome,
// refused or unknown, that a factory older than it never sends and that is
// absent from every other refusal.
func TestFactoryRefusalCarriesTheCommentOutcome(t *testing.T) {
	for want, detail := range map[string]FactoryRefusal{
		`"outcome":"refused"`: {Reason: "in_flight", Outcome: "refused"},
		`"outcome":"unknown"`: {Outcome: "unknown"},
	} {
		raw, _ := json.Marshal(detail)
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s lacks %s", raw, want)
		}
	}
	if raw, _ := json.Marshal(FactoryRefusal{Reason: "stale"}); strings.Contains(string(raw), "outcome") {
		t.Fatalf("a refusal with no outcome names one: %s", raw)
	}
}
