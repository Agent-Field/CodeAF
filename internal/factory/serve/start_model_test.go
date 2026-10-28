package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
)

// A start whose model is on a service with no key, and with no keyed
// fallback, is refused before any write with what to do; a key lets it start.
func TestStartRefusesAModelWithNoKeyBeforeItWrites(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	f, profile := keylessSetup(t, factoryapi.FactorySolo)
	launched := 0
	f.seam.Launch = func(int) error { launched++; return nil }
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	it := f.add(t, "owner")
	before, _ := f.st.Get(it.ID)
	var refused struct {
		Error string `json:"error"`
	}
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", it.ID), "operator-secret", nil, 409, &refused)
	if !strings.Contains(refused.Error, "its model, anthropic/claude-sonnet-5.5, is on") || !strings.Contains(refused.Error, "which has no key") {
		t.Fatalf("refusal: %q", refused.Error)
	}
	if after, _ := f.st.Get(it.ID); !after.Changed.Equal(before.Changed) || launched != 0 {
		t.Fatal("a refused start wrote or launched")
	}
	if err := config.WriteAPIKey(profile, "sk-or-v1-"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", it.ID), "operator-secret", nil, 200, nil)
}

// A team server's ProfileDir is its own home, which holds none of its people's
// keys, and a job on automatic models may route roles to other keyed models:
// neither start is refused for a key.
func TestStartClaimsNoKeyOnATeamServerOrOnAutomaticModels(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	team, _ := keylessSetup(t, factoryapi.FactoryTeam)
	// No person yet, so the operator may start it and the start reaches its
	// refusals.
	it := team.add(t, "")
	if w := identityRequest(team.h, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", it.ID), "", "operator-secret", nil); w.Code != 200 {
		t.Fatalf("a team server refused the start: %d %s", w.Code, w.Body)
	}
	solo, _ := keylessSetup(t, factoryapi.FactorySolo)
	automatic := true
	auto, err := solo.st.Create(factory.Item{Title: "test work", Repo: "acme/api", State: factory.StateQueued, Assignee: "owner", AutomaticModels: &automatic})
	if err != nil {
		t.Fatal(err)
	}
	if w := identityRequest(solo.h, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", auto.ID), "", "operator-secret", nil); w.Code != 200 {
		t.Fatalf("a job on automatic models was refused: %d %s", w.Code, w.Body)
	}
}

// keylessSetup is a factory in mode whose profile holds no key, with a default
// model on a service that needs one.
func keylessSetup(t *testing.T, mode string) (*peopleV2Fixture, string) {
	t.Helper()
	f := peopleV2Setup(t, mode)
	profile := t.TempDir()
	f.cfg.ProfileDir = profile
	f.cfg.DefaultModel = func() string { return "anthropic/claude-sonnet-5.5" }
	f.seam.Launch = func(int) error { return nil }
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	return f, profile
}
