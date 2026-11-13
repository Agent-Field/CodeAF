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
// keys: its start is never refused for a key.
func TestStartClaimsNoKeyOnATeamServer(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	team, _ := keylessSetup(t, factoryapi.FactoryTeam)
	// No person yet, so the operator may start it and the start reaches its
	// refusals.
	it := team.add(t, "")
	if w := identityRequest(team.h, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", it.ID), "", "operator-secret", nil); w.Code != 200 {
		t.Fatalf("a team server refused the start: %d %s", w.Code, w.Body)
	}
}

// Only the job's own model counts: a request with no key is reported, never
// moved to a fallback, so a keyed fallback does not let the start go, whatever
// the job's automatic-models choice.
func TestStartCountsOnlyTheJobsOwnModel(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	on, off := true, false
	for _, tc := range []struct {
		name      string
		automatic *bool
	}{{"a job that never chose", nil}, {"automatic models", &on}, {"automatic models off", &off}} {
		t.Run(tc.name, func(t *testing.T) {
			f, profile := keylessSetup(t, factoryapi.FactorySolo)
			// DeepSeek holds a key; the job's model is on OpenRouter, which
			// holds none.
			if err := config.WriteSources(profile, []config.PersistedSource{{ID: "deepseek", Written: "deepseek", Key: "sk-deepseek-test-0000000001", Order: 1}}); err != nil {
				t.Fatal(err)
			}
			row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(config.KeyModelFallbacks)
			if !ok {
				t.Fatal("no fallbacks row")
			}
			if err := row.Apply("deepseek/b"); err != nil {
				t.Fatal(err)
			}
			it, err := f.st.Create(factory.Item{Title: "test work", Repo: "acme/api", State: factory.StateQueued, Assignee: "owner", AutomaticModels: tc.automatic})
			if err != nil {
				t.Fatal(err)
			}
			if w := identityRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", it.ID), "", "operator-secret", nil); w.Code != 409 {
				t.Fatalf("got %d %s, want the key refusal", w.Code, w.Body)
			}
			// A job whose own model has a key starts.
			keyed, err := f.st.Create(factory.Item{Title: "test work", Repo: "acme/api", State: factory.StateQueued, Assignee: "owner", Model: "deepseek/b", AutomaticModels: tc.automatic})
			if err != nil {
				t.Fatal(err)
			}
			if w := identityRequest(f.h, "POST", fmt.Sprintf("/v1/jobs/%d/start_now", keyed.ID), "", "operator-secret", nil); w.Code != 200 {
				t.Fatalf("a keyed model was refused: %d %s", w.Code, w.Body)
			}
		})
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
