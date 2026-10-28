package session

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// A request on a service with no key fails before it is sent, and the turn
// reports it: it never moves to a fallback model, even with one available and
// out of time. A factory's start check counts only the job's own model on this
// (internal/factory/serve's startModelRefused).
func TestARequestWithNoKeyIsReportedNeverHopped(t *testing.T) {
	e := wireEvidence(fmt.Errorf("send: %w", provider.ErrNoAPIKey), 1)
	e.FallbackAvailable = true
	for _, outOfTime := range []bool{false, true} {
		e.OutOfTime = outOfTime
		if v := taxonomy.Classify(e, taxonomy.Limits{}); v.Action == taxonomy.ActionHop || v.Action == taxonomy.ActionRetry {
			t.Fatalf("out of time %v: a request with no key was answered %v", outOfTime, v)
		}
	}
}
