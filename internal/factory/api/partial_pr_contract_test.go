package api

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestPartialPRProblemsSurviveWireWithoutLosingCI(t *testing.T) {
	pr := &factory.PullRequest{Number: 7, State: factory.PROpen, Draft: true, ChecksRead: true, ReviewsRead: true, Checks: factory.PRChecks{Passed: 8}, Problems: []factory.PRReadProblem{{Area: "readiness", Code: "access_denied", Summary: "Policy access denied", Detail: "HTTP 403", Retryable: false}}}
	raw, e := json.Marshal(fromPR(pr))
	if e != nil {
		t.Fatal(e)
	}
	var wire PR
	if e = json.Unmarshal(raw, &wire); e != nil {
		t.Fatal(e)
	}
	got := toPR(&wire)
	if !got.ChecksRead || got.Checks.Passed != 8 || len(got.Problems) != 1 || got.Problems[0] != pr.Problems[0] {
		t.Fatal("partial facts lost in wire round trip")
	}
	if e = json.Unmarshal([]byte(`{"number":7,"draft":true}`), &wire); e != nil {
		t.Fatal(e)
	}
	var old PR
	_ = json.Unmarshal([]byte(`{"number":7,"draft":true}`), &old)
	if len(toPR(&old).Problems) != 0 {
		t.Fatal("old wire invented problem")
	}
}
