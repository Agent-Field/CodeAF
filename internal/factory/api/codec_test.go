package api

import (
"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
	"time"
)

func TestPRFeedbackTrackingAndCheckSourcesRoundTrip(t *testing.T) {
	p := &factory.PullRequest{FeedbackTracked: true, FeedbackItems: []factory.PRFeedback{{ID: "inline:1", Revision: "r1", Resolved: false}}, CheckRuns: []factory.CheckRun{{Source: "status", Name: "license/cla", State: "in_progress"}}}
	got := toPR(fromPR(p))
	if !got.FeedbackTracked || len(got.FeedbackItems) != 1 || got.FeedbackItems[0].ID != "inline:1" || got.CheckRuns[0].Source != "status" {
		t.Fatal("PR feedback or check provenance was lost")
	}
}

func TestVerificationPausePresentationNamesItsActualRecovery(t *testing.T) {
	j := Job{State: StateNeedsYou, FollowUp: &factory.PRFollowUp{State: "needs_you", BlockedBy: "verification", Reason: "Invalid test command"}, NeedsYou: &NeedsYou{Kind: factory.NeedsOutOfAttempts}}
	PresentJob(&j)
	if j.StatePresentation.Title != "Verification needs retry" {
		t.Fatal("verification problem labelled as a generic stuck job")
	}
}

// THE NO-CHECKS FACT CROSSES THE WIRE WHOLE, so a worker or an app reading the
// job judges readiness on the same settled fact the floor recorded.
func TestPRNoChecksFactRoundTrips(t *testing.T) {
	since := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	got := toPR(fromPR(&factory.PullRequest{NoWorkflows: true, NoChecksSince: since, NoChecks: true}))
	if !got.NoWorkflows || !got.NoChecksSince.Equal(since) || !got.NoChecks {
		t.Fatalf("the no-checks fact was lost on the wire: %+v", got)
	}
}

func TestLegacyPersonStopWirePresence(t *testing.T) {
 for _, raw:=range []string{
  `{"state":"needs_you","desk":{"engine_state":"new","stop_undo":{"By":"owner","Until":"2020-01-01T00:00:00Z"}}}`,
  `{"state":"needs_you","failure":{"code":"stopped","detail":"Stopped by you"},"desk":{"engine_state":"new"}}`,
 } {
  var job Job
  if err:=json.Unmarshal([]byte(raw),&job);err!=nil{t.Fatal(err)}
  if !factory.StoppedByPerson(ToItem(job)){t.Fatalf("old wire erased Stop: %s",raw)}
 }
 var granted Job
 if err:=json.Unmarshal([]byte(`{"state":"needs_you","failure":{"code":"stopped"},"desk":{"engine_state":"new","person_stopped":false}}`),&granted);err!=nil{t.Fatal(err)}
 if factory.StoppedByPerson(ToItem(granted)){t.Fatal("explicit person grant was replaced with legacy inference")}
}
