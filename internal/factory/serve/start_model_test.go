package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
)

// A start whose model is on a service with no key, and with no keyed
// fallback, is refused before any write with what to do; a key lets it start.
func TestStartRefusesAModelWithNoKeyBeforeItWrites(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	f := peopleV2Setup(t, factoryapi.FactorySolo)
	profile := t.TempDir()
	launched := 0
	f.seam.Launch = func(int) error { launched++; return nil }
	f.cfg.ProfileDir = profile
	f.cfg.DefaultModel = func() string { return "anthropic/claude-sonnet-5.5" }
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
