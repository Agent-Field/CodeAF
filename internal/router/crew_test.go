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
	onPlan.Repo = "other"
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
	for i := 0; i < redoDecayAfter; i++ {
		call := CrewCallID("")
		LogCrewDecision(dir, call, fix, nil)
		LogCrewOutcome(dir, call, fix, CrewAccepted, 0.01)
	}
	if got := ReadCrewLog(dir, time.Now()).Offsets["repo\x00bugfix"]; got != 0 {
		t.Errorf("after %d accepted fixes the offset is still %d", redoDecayAfter, got)
	}
	// Yesterday's spend is not today's.
	if got := ReadCrewLog(dir, time.Now().Add(48*time.Hour)); got.Tasks != 0 || got.SpentUSD != 0 {
		t.Errorf("two days on: %d tasks, $%v", got.Tasks, got.SpentUSD)
	}
}

// TODAY'S SPEND IS LAID ON THE PROVIDERS THAT CARRIED IT, a metered seat an
// even share, and a plan or local seat nothing.
func TestTheCrewLogSplitsTodayByProvider(t *testing.T) {
	dir := t.TempDir()
	mixed := CrewRecord{TaskClass: "bugfix", Seats: map[string]string{"worker": "a", "planner": "b", "checker": "c"},
		Providers: map[string]string{"worker": "openrouter", "planner": "deepseek", "checker": "codex"},
		Kinds:     map[string]string{"worker": "metered", "planner": "metered", "checker": "plan"}}
	LogCrewDecision(dir, "crew:m", mixed, nil)
	LogCrewOutcome(dir, "crew:m", mixed, CrewAccepted, 0.04)
	got := ReadCrewLog(dir, time.Now()).ProviderUSD
	if len(got) != 2 || got["openrouter"] != 0.02 || got["deepseek"] != 0.02 {
		t.Fatalf("by provider %v, want openrouter and deepseek at 0.02 each", got)
	}
	if _, ok := got["codex"]; ok {
		t.Fatal("a plan seat was charged a share")
	}
}

// A REDO'S OWN ACCEPTANCE KEEPS THE STEP IT TAUGHT: the offset a redo adds is
// for the NEXT task of the class there, and only a later accepted task takes
// it back off.
func TestARedoIsNotDecayedByItsOwnAcceptance(t *testing.T) {
	dir := t.TempDir()
	first := CrewRecord{TaskClass: "bugfix", Repo: "repo", Seats: map[string]string{"worker": "z-ai/glm-5.3-flash"}}
	LogCrewDecision(dir, "crew:a", first, nil)
	LogCrewOutcome(dir, "crew:a", first, CrewRedone, 0.01)
	redo := first
	redo.Redo = true
	LogCrewDecision(dir, "crew:b", redo, nil)
	LogCrewOutcome(dir, "crew:b", redo, CrewAccepted, 0.02)
	if got := ReadCrewLog(dir, time.Now()).Offsets["repo\x00bugfix"]; got != 1 {
		t.Fatalf("after the redo was accepted the offset is %d, want the step it taught", got)
	}
	LogCrewDecision(dir, "crew:c", first, nil)
	LogCrewOutcome(dir, "crew:c", first, CrewAccepted, 0.01)
	if got := ReadCrewLog(dir, time.Now()).Offsets["repo\x00bugfix"]; got != 0 {
		t.Errorf("a later accepted task left the offset at %d", got)
	}
}
