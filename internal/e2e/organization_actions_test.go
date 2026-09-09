package e2e

import (
	"encoding/json"
	"testing"
)

// Looking up current responsibilities does not create one. The report-only
// journeys allow this exact read while continuing to reject every mutation.
func organizationListsStanding(arguments string) bool {
	var call struct {
		Op string `json:"op"`
	}
	return json.Unmarshal([]byte(arguments), &call) == nil && call.Op == "list"
}

func TestOrganizationReportOnlyPermitsStandingLookup(t *testing.T) {
	if !organizationListsStanding(`{"op":"list"}`) {
		t.Fatal("read-only lookup was rejected")
	}
	for _, args := range []string{`{"op":"propose"}`, `{"op":"pause"}`, `{"op":"resume"}`, `{"op":"stop"}`, `{"op":"change"}`, `{}`, `invalid`} {
		if organizationListsStanding(args) {
			t.Fatalf("non-read operation accepted: %s", args)
		}
	}
}
