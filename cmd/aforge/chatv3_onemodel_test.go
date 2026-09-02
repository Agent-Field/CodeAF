package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// --one-model is the measurement posture: a run whose spend and quality are
// being attributed to one model cannot have a tier row answering a quarter of
// its calls on another. A benchmark cell measured this way is what these tests
// protect, and the second half of each is the promise that matters more — the
// flag OFF reads the rows exactly as it did before the flag existed.

// The profile every case here starts from: every text row pointing somewhere
// other than the session model, so a row that leaks is a row that is visible.
func oneModelProfile(t *testing.T) string {
	t.Helper()
	return v3Profile(t, map[string]any{
		"models.roles":            "planner:pinned/planner\ndesigner:pinned/designer",
		"models.tiers.low":        "tier/low",
		"models.tiers.high":       "tier/high",
		"models.tiers.reflex":     "tier/reflex",
		"models.tiers.mastermind": "tier/mastermind",
		"task.model":              "task/model",
		"models.fallbacks":        "fallback/one,fallback/two",
	})
}

func TestOneModelSettlesEveryTextSlotOnTheSessionModel(t *testing.T) {
	cfg, err := applyV3Governance(
		session.Config{Model: "session/model"}, oneModelProfile(t), false, true)
	if err != nil {
		t.Fatalf("the rows did not load: %v", err)
	}

	// THE LADDER. A nil source is the ladder's own "nothing is set", so every
	// role falls through pin and tier to the session model.
	source := roles.Source(cfg.RolesSource)
	for _, role := range []roles.Role{roles.RolePlanner, roles.RoleDesigner} {
		model, err := roles.Resolve(source, role, "session/model")
		if err != nil {
			t.Fatalf("role %q did not resolve: %v", role, err)
		}
		if model != "session/model" {
			t.Errorf("role %q ran on %q, not the session model", role, model)
		}
	}
	if model, ok := roles.Pinned(source, roles.RolePlanner); ok {
		t.Errorf("a pin survived --one-model: planner on %q", model)
	}
	if model, ok := roles.TierModel(source, roles.TierHigh); ok {
		t.Errorf("a tier survived --one-model: high on %q", model)
	}

	// THE TWO ROWS THAT ARE NOT THE LADDER. An empty task model reads the live
	// conversation model; an empty chain hops nowhere.
	if cfg.TaskModel != "" {
		t.Errorf("the task model stayed at %q; empty is what reads the live model", cfg.TaskModel)
	}
	if len(cfg.ModelFallbacks) != 0 {
		t.Errorf("the fallback chain survived --one-model: %v", cfg.ModelFallbacks)
	}
	// AND THE CATALOG'S GUESS, which is the same question asked a second way.
	// The chain falls through to this seam when no row is written, so a run
	// measured as one model would have walked to two models a similarity table
	// picked (internal/provider's fallbackChain).
	if cfg.NearestModels != nil {
		t.Error("the catalog's nearest-model guess survived --one-model")
	}

	// AND THE FLAG ITSELF TRAVELS, which is the row the withheld ladder cannot
	// stand in for. Two errands hand the ladder an empty floor on purpose — the
	// mark's reader and the brief's writer are crew-only — so under an empty
	// ladder they resolve to no model at all rather than to the session's, and
	// the session is the only place that can know better (#443).
	if !cfg.OneModel {
		t.Error("the flag did not reach the session, so the crew-only rungs have no model")
	}
}

// The half that protects everybody who never types the flag.
func TestWithoutOneModelEveryRowStillAnswers(t *testing.T) {
	cfg, err := applyV3Governance(
		session.Config{Model: "session/model"}, oneModelProfile(t), false, false)
	if err != nil {
		t.Fatalf("the rows did not load: %v", err)
	}
	source := roles.Source(cfg.RolesSource)
	if model, ok := roles.Pinned(source, roles.RolePlanner); !ok || model != "pinned/planner" {
		t.Errorf("the planner pin read as %q (ok=%v), want pinned/planner", model, ok)
	}
	if model, ok := roles.TierModel(source, roles.TierHigh); !ok || model != "tier/high" {
		t.Errorf("the high tier read as %q (ok=%v), want tier/high", model, ok)
	}
	if cfg.TaskModel != "task/model" {
		t.Errorf("the task model read as %q, want task/model", cfg.TaskModel)
	}
	if len(cfg.ModelFallbacks) == 0 {
		t.Error("the fallback chain did not survive a run without the flag")
	}
	if cfg.OneModel {
		t.Error("a run that never typed the flag carries it into the session")
	}
}

// A malformed pins row stops a launch that cares what the pins say, and does
// not stop one that has already said it does not.
func TestOneModelDoesNotReadTheRowsItIgnores(t *testing.T) {
	broken := v3Profile(t, map[string]any{"models.roles": "this is not a pins row"})
	if _, err := applyV3Governance(
		session.Config{Model: "session/model"}, broken, false, false); err == nil {
		t.Fatal("a malformed pins row launched without the flag")
	}
	if _, err := applyV3Governance(
		session.Config{Model: "session/model"}, broken, false, true); err != nil {
		t.Fatalf("--one-model stopped on a row it does not read: %v", err)
	}
}
