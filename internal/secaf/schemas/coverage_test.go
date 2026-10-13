package schemas

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIncompleteHuntCoverageSurvivesReportJSON(t *testing.T) {
	want := HuntCoverage{Attempted: 3, Failures: []HuntFailure{{Strategy: "auth", Reason: "no reply"}}}
	result := NewSecurityAuditResult()
	result.Metadata["hunt_coverage"] = want
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var back SecurityAuditResult
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if got := Coverage(back.Metadata); !reflect.DeepEqual(got, want) {
		t.Fatalf("coverage=%+v want=%+v", got, want)
	}
}

func TestUnreadableHuntCoverageCannotLookComplete(t *testing.T) {
	for _, raw := range []any{nil, "bad", map[string]any{}, map[string]any{"attempted": 1}, map[string]any{"attempted": 1, "failures": nil}, map[string]any{"attempted": 1, "failures": []any{1, 2}}} {
		if !Coverage(map[string]any{"hunt_coverage": raw}).Incomplete() {
			t.Fatalf("unreadable coverage looks complete: %#v", raw)
		}
	}
	if Coverage(nil).Incomplete() {
		t.Fatal("a complete legacy report became incomplete")
	}
}
