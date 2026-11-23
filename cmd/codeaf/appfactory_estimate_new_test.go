package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A NEW JOB'S ESTIMATE, AGAINST THE REAL SERVER AND STORE: five finished
// issue jobs nobody read are the figure for the next issue job in the same
// repository, before it exists.
func TestAppFactoryEstimateNewAgainstTheRealServer(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	alice.w.mu.Lock()
	hello := alice.w.lines[0]
	alice.w.mu.Unlock()
	if !strings.Contains(hello, appwire.FeatureFactoryEstimateNew) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryEstimateNew)
	}
	start := f.deskNow().Add(-48 * time.Hour)
	for i := range 5 {
		at := start.Add(time.Duration(i) * time.Hour)
		f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateShipped, DoneHow: "answered",
			Stream: &factory.Stream{Spent: 0.4, Started: at, Ended: at.Add(15 * time.Minute)}})
	}
	var got api.NewEstimate
	alice.w.must("factory.estimate_new", map[string]any{"factory": alice.conn.ID, "repo": "acme/api", "kind": "issue"}, &got)
	if e := got.Estimate; e == nil || e.N != 5 || e.USDHi != 0.4 || e.MinHi != 15 || e.Basis != "kind" || e.Repo != "acme/api" {
		t.Fatalf("estimate = %+v (%+v)", got, got.Estimate)
	}
}
