package session

import "testing"

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
		{beltRunOutcomeDone, RunLanding{Home: mergeKept, Refused: "the checker said no"}, false},
		{"failed", RunLanding{Home: mergeKept}, false},
	}
	for _, tc := range cases {
		if got := crewKept(tc.outcome, tc.landing); got != tc.kept {
			t.Errorf("%s / %+v: kept %v, want %v", tc.outcome, tc.landing, got, tc.kept)
		}
	}
}
