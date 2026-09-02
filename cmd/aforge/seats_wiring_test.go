package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ONE LADDER, EVERY HEADLESS DOOR, AND NO SECOND COPY OF IT.
//
// The defect this guards was not a wrong rung — it was four commands each
// resolving their two models their own way, so a crew that reached the chat
// reached none of them (#166). The next door will be written by copying one of
// these, and the copy is only safe while the resolution is a call rather
// than a paragraph worth of lookups. So this reads the source: every door asks
// config.ResolveSeats, and no door reaches past it for the environment or the
// build's default on its own.
func TestEveryHeadlessDoorResolvesItsSeatsThroughTheOneLadder(t *testing.T) {
	// main.go carries two doors — plan and revise — and the other three files
	// carry one each.
	doors := map[string]int{
		"do.go": 1, "exec.go": 1, "run.go": 1, "main.go": 2, "subharness_run.go": 1,
	}
	for name, wanted := range doors {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if got := strings.Count(source, "config.ResolveSeats("); got != wanted {
			t.Errorf("%s climbs the ladder %d times, want %d — a door that resolves its models "+
				"another way is a door the crew does not reach", name, got, wanted)
		}
		// A door either seats the answer on its own settings or hands it to the
		// brain that does (brainOptions.seats). What it may not do is resolve
		// the seats and then build its clients from something else.
		if !strings.Contains(source, "applySeats(") && !strings.Contains(source, "seats:") {
			t.Errorf("%s resolves the seats and never seats them", name)
		}
		for _, reach := range []string{
			`os.Getenv("AFORGE_MODEL")`,
			`os.Getenv("AFORGE_PLAN_MODEL")`,
			"os.Getenv(config.ModelEnv)",
			"os.Getenv(config.PlanModelEnv)",
		} {
			if strings.Contains(source, reach) {
				t.Errorf("%s reads %s for itself; the ladder reads the environment, "+
					"and it is the only rung that can tell the environment from the default", name, reach)
			}
		}
	}

	// The build's default is the ladder's bottom rung and nothing else's. It
	// survives in main.go exactly once — in the usage table, where the row for
	// AFORGE_MODEL prints the default it falls back to — and that is prose, not
	// resolution.
	for _, name := range []string{"do.go", "exec.go", "run.go", "subharness_run.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "config.DefaultModel") {
			t.Errorf("%s names the build's default model itself instead of falling to the ladder's last rung", name)
		}
	}
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "config.DefaultModel") && !strings.Contains(line, "AFORGE_MODEL") {
			t.Errorf("main.go reads the build's default outside the usage table: %q", strings.TrimSpace(line))
		}
	}
}

// The help text is where a person learns that their crew reaches this command,
// so it names the whole ladder rather than one rung of it.
func TestTheModelFlagsNameTheWholeLadder(t *testing.T) {
	for _, name := range []string{"do.go", "exec.go", "run.go", "main.go", "subharness_run.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "run (default AFORGE_MODEL)") {
			t.Errorf("%s still says the work model defaults to AFORGE_MODEL, which was one rung of four", name)
		}
	}
	for _, want := range []string{"AFORGE_MODEL", "crew", "default"} {
		if !strings.Contains(modelFlagHelp, want) {
			t.Errorf("--model's help does not mention %q: %q", want, modelFlagHelp)
		}
	}
	if !strings.Contains(planModelFlagHelp, "AFORGE_PLAN_MODEL") || !strings.Contains(planModelFlagHelp, "crew") {
		t.Errorf("--plan-model's help does not name its own ladder: %q", planModelFlagHelp)
	}
}

// THE ACCEPTANCE CASE, END TO END: a profile that says `frugal` and a command
// line that says nothing.
//
// The errand must plan on the profile's mastermind and work on its working tier
// — the two seats the chat's planner and worker ride (roles.DefaultAssignment)
// — and it must SAY SO, on the opening line and in the object a harness reads.
// Both halves matter: a benchmark that cannot read back which crew ran is the
// position this issue was reported from.
func TestAnErrandWithNoFlagsRunsTheProfilesCrewAndSaysSo(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	// The variables are cleared rather than left to the shell this suite runs
	// in: the rung under test is the one below them.
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")
	if err := config.ApplyCrew(script.dir, config.CrewFrugal); err != nil {
		t.Fatalf("writing the frugal crew: %v", err)
	}

	var mu sync.Mutex
	var built []string
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
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

	work := config.TierModelAt(script.dir, config.ModelTierLow)
	plan := config.TierModelAt(script.dir, config.ModelTierMastermind)
	for _, model := range []string{work, plan} {
		found := false
		mu.Lock()
		for _, built := range built {
			found = found || built == model
		}
		models := append([]string(nil), built...)
		mu.Unlock()
		if !found {
			t.Fatalf("no client was built on %q; the run used %v", model, models)
		}
	}

	if line := config.ResolveSeats(script.dir, "", "").Line(); !strings.Contains(stderr.String(), line) {
		t.Fatalf("the opening lines never named the crew:\nwant %q\nstderr:\n%s", line, stderr.String())
	}
	if !strings.Contains(stderr.String(), "crew frugal") {
		t.Fatalf("the receipt did not name the preset:\n%s", stderr.String())
	}

	var outcome headlessOutcome
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("--json did not print one object: %v\n%s", err, stdout.String())
	}
	if outcome.Model != work || outcome.PlanModel != plan {
		t.Fatalf("--json named model %q and plan_model %q, want %q and %q",
			outcome.Model, outcome.PlanModel, work, plan)
	}
	if outcome.ModelSource != "crew frugal" || outcome.PlanModelSource != "crew frugal" {
		t.Fatalf("--json named the rungs %q and %q, want the crew for both",
			outcome.ModelSource, outcome.PlanModelSource)
	}
}

// The flag still wins, and the receipt still says which rung answered — the two
// halves of the ladder that a benchmark pinning one seat depends on.
func TestAFlaggedSeatOutranksTheCrewAndTheReceiptSaysWhich(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")
	if err := config.ApplyCrew(script.dir, config.CrewFrugal); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:      "write the release note and include the migration steps",
		timeout:   60 * time.Second,
		asJSON:    true,
		model:     "vendor/pinned-worker",
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
	if outcome.Model != "vendor/pinned-worker" || outcome.ModelSource != "--model" {
		t.Fatalf("the flagged seat reads %q (%s)", outcome.Model, outcome.ModelSource)
	}
	// And the seat nobody flagged still climbs to the crew.
	if outcome.PlanModelSource != "crew frugal" {
		t.Fatalf("the plan seat reads %q (%s)", outcome.PlanModel, outcome.PlanModelSource)
	}
}

// A tier value may carry a thinking level, and it must travel exactly as far as
// a flag carrying one does: whole, into the seat and into the plan role's
// binding, where the ladder splits it into a model and an effort at the point of
// the call. The balanced crew's mastermind is exactly that value, so this is the
// road's own check that a headless run on `balanced` plans at `low` — which is
// what the same crew does in the conversation.
func TestACrewsThinkingLevelReachesTheRunWhole(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")
	if err := config.ApplyCrew(script.dir, config.CrewBalanced); err != nil {
		t.Fatal(err)
	}
	written := config.TierModelAt(script.dir, config.ModelTierMastermind)
	if _, level := roles.SplitEffort(written); level == "" {
		t.Fatalf("the balanced mastermind reads %q and carries no level, so this test is about nothing", written)
	}

	// A durable store, because the plan role's binding is the record under test
	// and an ephemeral one evaporates with the run.
	database := filepath.Join(t.TempDir(), "graph.db")
	var mu sync.Mutex
	var built []string
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:     "write the release note and include the migration steps",
		database: database,
		timeout:  60 * time.Second,
		asJSON:   true,
		stdout:   &stdout,
		stderr:   &stderr,
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

	mu.Lock()
	models := append([]string(nil), built...)
	mu.Unlock()
	found := false
	for _, model := range models {
		found = found || model == written
	}
	if !found {
		t.Fatalf("the plan seat was filled with %v, none of them the crew's own %q", models, written)
	}

	// The binding the ladder resolves calls through carries the level, and says
	// the crew put it there rather than naming a variable nobody set.
	graph, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	binding, ok, err := graph.RoleBindingAt(store.RolePlan, store.ScopeGlobal)
	if err != nil || !ok {
		t.Fatalf("plan binding: found=%t err=%v", ok, err)
	}
	if binding.Value != written {
		t.Fatalf("the plan role is bound to %q, want the crew's %q", binding.Value, written)
	}
	if binding.Origin != store.RoleSeedOriginPrefix+"crew balanced" {
		t.Fatalf("the binding says %q named the model", binding.Origin)
	}

	// And what the run reports is what the person wrote in the sheet.
	var outcome headlessOutcome
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("--json did not print one object: %v\n%s", err, stdout.String())
	}
	if outcome.PlanModel != written {
		t.Fatalf("--json named plan_model %q, want %q", outcome.PlanModel, written)
	}
	if !strings.Contains(stderr.String(), "plan "+written+" (crew balanced)") {
		t.Fatalf("the receipt did not print the value as the sheet holds it:\n%s", stderr.String())
	}
}

// THE WHOLE ROAD, ON A PROFILE OLDER THAN THE WORKER SEAT (#302).
//
// The unit test pins the rung; this pins what a person actually gets: a config
// written before the worker row existed — the four keys and no fifth — must send
// every client the errand builds to the crew's own models, never to the build's
// default, and the run must SAY on its way past that the seat was inherited. The
// second half is the part that makes the first half checkable from outside,
// which is the property the defect took away.
func TestAnErrandOnACrewOlderThanTheWorkerSeatNeverTouchesTheBuildsDefault(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.PlanModelEnv, "")

	// The pre-#278 crew shape, written as a profile of that vintage holds it.
	pinned := "vendor/pinned-small-work"
	profile := map[string]string{
		config.KeyTierReflexModel:     "vendor/pinned-reflex",
		config.KeyTierLowModel:        pinned,
		config.KeyTierHighModel:       "vendor/pinned-careful",
		config.KeyTierMastermindModel: "vendor/pinned-thinking",
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(script.dir), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var built []string
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
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
	}); err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstderr:\n%s", err, stderr.String())
	}

	mu.Lock()
	models := append([]string(nil), built...)
	mu.Unlock()
	for _, model := range models {
		if model == config.DefaultModel {
			t.Fatalf("a client was built on the build's default %q; the run used %v",
				config.DefaultModel, models)
		}
	}
	worked := false
	for _, model := range models {
		worked = worked || model == pinned
	}
	if !worked {
		t.Fatalf("nothing ran on the small-work model the crew pinned; the run used %v", models)
	}

	// The receipt: the work seat says the crew answered and that the row was
	// inherited, and the one line saying why is printed ONCE.
	seats := config.ResolveSeats(script.dir, "", "")
	if !strings.Contains(stderr.String(), "work "+pinned+" (crew custom, inherited)") {
		t.Fatalf("the opening line does not name the inherited seat:\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), "work "+config.DefaultModel) {
		t.Fatalf("the opening line still seats the build's default:\n%s", stderr.String())
	}
	notice := seats.Notice()
	if notice == "" {
		t.Fatal("the seats resolved by inheritance and the run has nothing to say about it")
	}
	if got := strings.Count(stderr.String(), notice); got != 1 {
		t.Fatalf("the run said the line %d times, want once:\n%s", got, stderr.String())
	}

	// And the object a script reads carries the same fact.
	var outcome headlessOutcome
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("--json did not print one object: %v\n%s", err, stdout.String())
	}
	if outcome.Model != pinned || outcome.ModelSource != "crew custom, inherited" {
		t.Fatalf("--json named model %q (%s), want the inherited crew row",
			outcome.Model, outcome.ModelSource)
	}
}
