package session

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/router"
)

// A FINISHED RUN NOBODY REFUSED IS ACCEPTED — a deliberate keep on its branch
// and an answer with nothing to land included — so the redo offset decays on
// work that went well rather than climbing for ever.
func TestACrewsWorkIsKeptUnlessItsLandingWasRefused(t *testing.T) {
	cases := []struct {
		outcome string
		landing RunLanding
		kept    bool
	}{
		{beltRunOutcomeDone, RunLanding{Home: mergeMerged}, true},
		{beltRunOutcomeDone, RunLanding{Home: mergeInPlace}, true},
		{beltRunOutcomeDone, RunLanding{Home: mergeKept}, true},
		{beltRunOutcomeDone, RunLanding{}, true},
		{beltRunOutcomeDone, RunLanding{Home: mergeConflicted}, false},
		{beltRunOutcomeDone, RunLanding{Home: mergeAborted}, false},
		{beltRunOutcomeDone, RunLanding{Home: mergeKept, Branch: "task/fix", Refused: "its work is kept on task/fix and did not go into main"}, true},
		{beltRunOutcomeDone, RunLanding{Refused: "the checker said no"}, false},
		{"failed", RunLanding{Home: mergeKept}, false},
	}
	for _, tc := range cases {
		if got := crewKept(tc.outcome, tc.landing); got != tc.kept {
			t.Errorf("%s / %+v: kept %v, want %v", tc.outcome, tc.landing, got, tc.kept)
		}
	}
}

// A TASK KEPT ON ITS BRANCH TAKES A REDO'S STEP BACK OFF. The landing is the
// exact shape [Agent.landBeltRun] hands back for a deliberate keep — the keep
// sentence in Refused — and the offset a redo left must decay on it.
func TestAKeptBranchIsAcceptedAndDecaysTheRedoOffset(t *testing.T) {
	dir := t.TempDir()
	record := router.CrewRecord{TaskClass: "bugfix", Repo: "repo", Seats: map[string]string{"worker": "z-ai/glm-5.3-flash"}}
	router.LogCrewDecision(dir, "crew:a", record, nil)
	router.LogCrewOutcome(dir, "crew:a", record, router.CrewRedone, 0.02)
	if got := router.ReadCrewLog(dir, time.Now()).Offsets["repo\x00bugfix"]; got != 1 {
		t.Fatalf("a redo left an offset of %d", got)
	}
	landing := RunLanding{Home: mergeKept, Branch: "task/fix", Refused: "its work is kept on task/fix and did not go into main"}
	outcome := router.CrewNotKept
	if crewKept(beltRunOutcomeDone, landing) {
		outcome = router.CrewAccepted
	}
	router.LogCrewDecision(dir, "crew:b", record, nil)
	router.LogCrewOutcome(dir, "crew:b", record, outcome, 0.01)
	if got := router.ReadCrewLog(dir, time.Now()).Offsets["repo\x00bugfix"]; got != 0 {
		t.Errorf("a task kept on its branch left the offset at %d (outcome %q)", got, outcome)
	}
}
