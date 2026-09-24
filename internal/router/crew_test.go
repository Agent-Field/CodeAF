package router

import (
	"testing"
	"time"
)

func TestTheCrewLogKeepsTodayAndLearnsFromRedos(t *testing.T) {
	dir := t.TempDir()
	fix := CrewRecord{TaskClass: "bugfix", Repo: "repo", Seats: map[string]string{"worker": "z-ai/glm-5.3-flash"},
		Kinds: map[string]string{"worker": "metered"}}
	onPlan := fix
	onPlan.Kinds = map[string]string{"worker": "plan"}
	LogCrewDecision(dir, "crew:a", fix, []string{"z-ai/glm-5.3-flash"})
	LogCrewOutcome(dir, "crew:a", fix, CrewRedone, 0.02)
	LogCrewDecision(dir, "crew:b", onPlan, nil)
	LogCrewOutcome(dir, "crew:b", onPlan, CrewAccepted, 0)
	LogCrewDecision(dir, "crew:c", fix, nil)

	log := ReadCrewLog(dir, time.Now())
	if log.Tasks != 3 || log.OnPlan != 1 {
		t.Errorf("today: %d tasks, %d on a plan; want 3 and 1", log.Tasks, log.OnPlan)
	}
	if log.SpentUSD != 0.02 {
		t.Errorf("today's spend %v, want the settled 0.02", log.SpentUSD)
	}
	if got := log.Offsets["repo\x00bugfix"]; got != 1 {
		t.Errorf("one redo left an offset of %d", got)
	}
	if len(log.Recent) != 3 || log.Recent[0].Call != "crew:c" || log.Recent[0].Settled {
		t.Errorf("recent %+v, want the unsettled c first", log.Recent)
	}
	// Enough accepted fixes take the step back off.
	for i := 0; i < crewDecayAfter; i++ {
		call := CrewCallID("")
		LogCrewDecision(dir, call, fix, nil)
		LogCrewOutcome(dir, call, fix, CrewAccepted, 0.01)
	}
	if got := ReadCrewLog(dir, time.Now()).Offsets["repo\x00bugfix"]; got != 0 {
		t.Errorf("after %d accepted fixes the offset is still %d", crewDecayAfter, got)
	}
	// Yesterday's spend is not today's.
	if got := ReadCrewLog(dir, time.Now().Add(48*time.Hour)); got.Tasks != 0 || got.SpentUSD != 0 {
		t.Errorf("two days on: %d tasks, $%v", got.Tasks, got.SpentUSD)
	}
}
