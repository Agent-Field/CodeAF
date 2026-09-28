package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// ONE LADDER, EVERY HEADLESS DOOR, AND NO SECOND COPY OF IT.
//
// The defect this guards was not a wrong rung — it was four commands each
// resolving their two models their own way, so a crew that reached the chat
// reached none of them (#166). The next door will be written by copying one of
// these, and the copy is only safe while the resolution is a call rather
// than a paragraph worth of lookups. So this reads the source: `codeaf do`
// asks config.ResolveSeats itself, every other door asks doorSeats (which asks
// it once), and no door reaches past it for the environment on its own.
func TestEveryHeadlessDoorResolvesItsSeatsThroughTheOneLadder(t *testing.T) {
	ladder := map[string]int{"do.go": 1, "main.go": 1}
	doors := map[string]int{"exec.go": 1, "run.go": 1, "main.go": 3, "subharness_run.go": 1}
	for name, wanted := range ladder {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(raw), "config.ResolveSeats("); got != wanted {
			t.Errorf("%s climbs the ladder %d times, want %d", name, got, wanted)
		}
	}
	for name, wanted := range doors {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(raw), "doorSeats("); got != wanted {
			t.Errorf("%s asks doorSeats %d times, want %d — a door that resolves its models "+
				"another way is a door the crew does not reach", name, got, wanted)
		}
	}
	for _, name := range []string{"do.go", "exec.go", "run.go", "main.go", "subharness_run.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if !strings.Contains(source, "applySeats(") && !strings.Contains(source, "seats:") && !strings.Contains(source, "seats.Work.Model") {
			t.Errorf("%s resolves the seats and never seats them", name)
		}
		for _, reach := range []string{
			`os.Getenv("CODEAF_MODEL")`,
			`os.Getenv("CODEAF_PLAN_MODEL")`,
			`os.Getenv("CODEAF_CHECK_MODEL")`,
			"os.Getenv(config.ModelEnv)",
			"os.Getenv(config.PlanModelEnv)",
		} {
			if strings.Contains(source, reach) {
				t.Errorf("%s reads %s for itself; the ladder reads the environment", name, reach)
			}
		}
		// NO SEAT FALLS TO A MODEL THIS BUILD CHOSE FOR EVERYBODY.
		if name != "main.go" && strings.Contains(source, "config.DefaultModel") {
			t.Errorf("%s names the build's default model itself", name)
		}
	}
}

// The help text is where a person learns that the flag is a one-task pin and
// that the crew is routed per task when nothing is pinned.
func TestTheModelFlagsNameTheWholeLadder(t *testing.T) {
	for _, help := range []string{modelFlagHelp, planModelFlagHelp, checkModelFlagHelp} {
		for _, want := range []string{"one-task pin", "crew pin", "routed per task"} {
			if !strings.Contains(help, want) {
				t.Errorf("a model flag's help does not say %q: %q", want, help)
			}
		}
	}
	if !strings.Contains(checkModelFlagHelp, "CODEAF_CHECK_MODEL") {
		t.Errorf("--check-model's help does not name its variable: %q", checkModelFlagHelp)
	}
	// THE CHECK SEAT'S LADDER NEVER PASSES THROUGH THE PLAN SEAT.
	if strings.Contains(checkLadderHelp, "plan") {
		t.Errorf("the check seat's ladder still names the plan seat: %q", checkLadderHelp)
	}
}

// crewDoorCatalog is a catalog the router can price: a cheap model and a
// dear, better one, both open and both serving tools.
func crewDoorCatalog() []catalog.Model {
	return []catalog.Model{
		{ID: "vendor/cheap", PromptPrice: 0.1e-6, CompletionPrice: 0.4e-6, CodingIndex: 40, AgenticIndex: 40, IntelligenceIndex: 40,
			Parameters: []string{"tools"}, ContextLength: 200000, OpenWeights: true},
		{ID: "vendor/strong", PromptPrice: 3e-6, CompletionPrice: 15e-6, CodingIndex: 70, AgenticIndex: 70, IntelligenceIndex: 70,
			Parameters: []string{"tools"}, ContextLength: 200000, OpenWeights: true},
	}
}

// seatCrewCatalog points the router at a catalog for one test.
func seatCrewCatalog(t *testing.T, rows []catalog.Model) {
	t.Helper()
	previous, seat := config.CrewCatalog, seatCrewRows
	config.CrewCatalog = func() []catalog.Model { return rows }
	seatCrewRows = func(func() []catalog.Model) {}
	t.Cleanup(func() { config.CrewCatalog, seatCrewRows = previous, seat })
}

// THE ACCEPTANCE CASE, END TO END: a command line that says nothing, on a
// profile that pinned only the checker.
//
// The worker and the planner are ROUTED for this task and the checker is the
// pin, and the run says so — on the opening lines, in the summary line with the
// actual beside the estimate, and in the object a harness reads (`class`,
// `crew`, `est_usd`, `check_model`). The decision and its outcome land in the
// router's log.
func TestAnErrandWithNoFlagsRunsARoutedCrewAndSaysSo(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")
	t.Setenv(config.CheckModelEnv, "")
	seatCrewCatalog(t, crewDoorCatalog())
	if err := config.SetCrewPin(script.dir, crewroute.Checker, "vendor/strong"); err != nil {
		t.Fatalf("pinning the checker: %v", err)
	}

	var mu sync.Mutex
	var built []string
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
		run:     "crew-door-run",
		timeout: 60 * time.Second,
		asJSON:  true,
		stdout:  &stdout,
		stderr:  &stderr,
		newClient: func(settings config.Config, model string) (*liveClient, error) {
			mu.Lock()
			built = append(built, model)
			mu.Unlock()
			return script.client(settings, model)
		},
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "crew: ") || !strings.Contains(stderr.String(), "checker strong (pinned)") || strings.Contains(stderr.String(), "📌") {
		t.Fatalf("the run never said its crew with the pinned checker marked:\n%s", stderr.String())
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &fields); err != nil {
		t.Fatalf("--json did not print one object: %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"class", "crew", "est_usd", "check_model", "model_source"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("--json carries no %q:\n%s", key, stdout.String())
		}
	}
	if fields["check_model"] != "vendor/strong" || fields["check_model_source"] != "pinned" {
		t.Fatalf("--json named the checker %v (%v), want the pin", fields["check_model"], fields["check_model_source"])
	}
	crew, ok := fields["crew"].(map[string]any)
	if !ok {
		t.Fatalf("--json crew is %T", fields["crew"])
	}
	checker, ok := crew["checker"].(map[string]any)
	if !ok || checker["pinned"] != true {
		t.Fatalf("--json checker pin is %v", crew["checker"])
	}
	if fields["model_source"] != "routed" {
		t.Fatalf("--json named the worker's rung %v, want routed", fields["model_source"])
	}

	log := router.ReadCrewLog(config.ProfilePath(script.dir, ""), time.Now())
	if len(log.Recent) != 1 || !log.Recent[0].Settled {
		t.Fatalf("the router's log holds %+v, want one settled crew", log.Recent)
	}
}

// The flag still wins, and it is a ONE-TASK PIN: the receipt says which rung
// answered, and the check seat does not inherit a flagged planner.
func TestAFlaggedPlannerNeverSeatsTheChecker(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")
	t.Setenv(config.CheckModelEnv, "")
	seatCrewCatalog(t, crewDoorCatalog())

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:      "write the release note and include the migration steps",
		timeout:   60 * time.Second,
		asJSON:    true,
		planModel: "vendor/flagged-planner",
		stdout:    &stdout,
		stderr:    &stderr,
		newClient: script.client,
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstderr:\n%s", err, stderr.String())
	}
	var outcome headlessOutcome
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("--json did not print one object: %v\n%s", err, stdout.String())
	}
	if outcome.PlanModel != "vendor/flagged-planner" || outcome.PlanModelSource != "--plan-model" {
		t.Fatalf("the flagged seat reads %q (%s)", outcome.PlanModel, outcome.PlanModelSource)
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(stdout.String()), &fields)
	if fields["check_model"] == "vendor/flagged-planner" {
		t.Fatal("the check seat inherited the flagged planner")
	}
}

// AT THE DAILY CAP `codeaf do` REFUSES, and -yes-spend is the one way past.
func TestAnErrandAtTheDailyCapRefusesUnlessToldToSpend(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")
	t.Setenv(config.CheckModelEnv, "")
	seatCrewCatalog(t, crewDoorCatalog())
	if err := config.SetCrewCap(script.dir, "1"); err != nil {
		t.Fatal(err)
	}
	previous := config.CrewHistory
	config.CrewHistory = func(string) config.CrewDay { return config.CrewDay{SpentUSD: 2} }
	t.Cleanup(func() { config.CrewHistory = previous })

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps", timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err == nil || !strings.Contains(err.Error(), "daily cap") {
		t.Fatalf("an errand at the cap was not refused: %v", err)
	}
	// THE REFUSAL NAMES ONLY THE WAYS ON THAT WORK: /crew cap, -yes-spend and
	// the day turning over. --cheap is refused at the cap too, so it is not one.
	for _, way := range []string{"/crew cap", "-yes-spend", "midnight"} {
		if !strings.Contains(err.Error(), way) {
			t.Errorf("the refusal does not name %q: %q", way, err)
		}
	}
	if strings.Contains(err.Error(), "--cheap") {
		t.Errorf("the refusal offers --cheap: %q", err)
	}
	err = doErrand(doRequest{
		task: "write the release note and include the migration steps", timeout: 60 * time.Second,
		effort: crewroute.EffortCheap, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if err == nil || !strings.Contains(err.Error(), "daily cap") {
		t.Fatalf("--cheap got past the cap: %v", err)
	}
	if script.count("chat") != 0 {
		t.Fatal("an errand at the cap spent before it refused")
	}
}

// --pin is repeatable, names a seat, and refuses what is not a pin.
func TestThePinFlagReadsSeatEqualsModel(t *testing.T) {
	var pins pinFlags
	if err := pins.Set("checker=moonshotai/kimi-k3@openrouter"); err != nil {
		t.Fatal(err)
	}
	if err := pins.Set("worker=z-ai/glm-5.3-flash"); err != nil {
		t.Fatal(err)
	}
	if got := pins.pins[crewroute.Checker]; got.Model != "moonshotai/kimi-k3" || got.Provider != "openrouter" {
		t.Fatalf("the checker pin reads %+v", got)
	}
	for _, bad := range []string{"judge=vendor/x", "worker", "worker=auto"} {
		if err := (&pinFlags{}).Set(bad); err == nil {
			t.Errorf("--pin %q was accepted", bad)
		}
	}
}
