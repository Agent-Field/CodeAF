package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A JOB'S READ SAYS HOW EACH TRY ENDED, as steps[].rounds[].result and
// .reason, and reads them back unchanged. A WORKER'S REPORT NEVER SENDS
// THEM: the factory judges each try from the report it folds, and a factory
// older than the fields decodes a worker's report strictly and would refuse
// the whole report for an unknown one.
func TestATrysResultReachesTheJobsReadAndStaysOutOfAWorkersReport(t *testing.T) {
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	rounds := []factory.RoundLine{
		{Round: 1, Rejected: true, Summary: "all good", At: at, Result: "not clean", Reason: "2 findings"},
		{Round: 2, Summary: "all good", At: at.Add(time.Minute), Result: "clean", Reason: "no findings"},
	}
	it := factory.Item{ID: 4, Repo: "acme/api", Num: 9, Kind: factory.KindIssue, State: factory.StateRunning,
		Stages: []factory.Stage{{Name: "review", Kind: factory.StageChat, Ask: "review it", Until: factory.UntilClean, Max: 2, On: true}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 2, Rounds: rounds}}}}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"result":"not clean","reason":"2 findings"`) {
		t.Fatalf("the read does not say how try 1 ended: %s", raw)
	}
	var read Job
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	_, stream := unsteps(read)
	if got := stream.Phases[0].Rounds; len(got) != 2 || got[0].Result != "not clean" || got[1].Reason != "no findings" {
		t.Fatalf("the read did not come back whole: %+v", got)
	}
	report, err := json.Marshal(FromRunReport(factory.RunReport{Stages: []factory.StageReport{{Name: "review", Round: 2, Rounds: rounds}}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(report), `"result"`) || strings.Contains(string(report), `"reason"`) {
		t.Fatalf("a worker's report sends the try's word: %s", report)
	}
}
