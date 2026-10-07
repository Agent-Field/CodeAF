package config

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// A PROGRAM'S WINDOW COMES FROM THE CATALOG CODEAF ALREADY KEPT. The listing
// the default service's catalog wrote answers a model in either spelling the
// person uses for it, a model it never listed answers zero, and nothing is
// asked of the network on the way.
func TestCachedContextWindowReadsTheProfilesOwnCatalog(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	profile := t.TempDir()
	t.Setenv(ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", profile)
	// The service's address is the stub, so a lookup that fetched would be
	// counted.
	t.Setenv("CODEAF_BASE_URL", server.URL+"/api/v1")
	if err := catalog.Remember(catalog.Options{BaseURL: server.URL + "/api/v1", Dir: profile}, []catalog.Model{
		{ID: "deepseek/deepseek-v4.1-flash", ContextLength: 1_048_576},
		{ID: "vendor/small", ContextLength: 16_384},
	}); err != nil {
		t.Fatal(err)
	}
	for model, want := range map[string]int{
		"deepseek/deepseek-v4.1-flash":            1_048_576,
		"openrouter/deepseek/deepseek-v4.1-flash": 1_048_576,
		"vendor/small":                            16_384,
		"vendor/never-listed":                     0,
		"":                                        0,
	} {
		if got := CachedContextWindow(model); got != want {
			t.Errorf("CachedContextWindow(%q) = %d, want %d", model, got, want)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("the lookup reached the network %d times", calls.Load())
	}
}

// A profile that never listed anything answers zero for every model, which
// is "nobody can say" and never a small window.
func TestCachedContextWindowOnAProfileWithNoCatalogIsZero(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", profile)
	if got := CachedContextWindow("deepseek/deepseek-v4.1-flash"); got != 0 {
		t.Fatalf("window = %d, want 0", got)
	}
}

// A model on another connected service is answered from THAT service's own
// compartment, found by the prefix the person writes it with, and never from
// the default service's list.
func TestCachedContextWindowReadsAConnectedServicesOwnCompartment(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_BASE_URL", "")
	if err := writeProfileValue(profile, keyModelSources, []PersistedSource{{ID: "z-ai", Written: "z-ai", Key: "zai-key-0123456789", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	var zai modelsource.Connected
	for _, service := range ResolveSources(profile, "", "").All() {
		if service.Source.ID == "z-ai" {
			zai = service
		}
	}
	if zai.Source.ID == "" {
		t.Fatal("the z-ai service did not resolve")
	}
	if err := catalog.Remember(CatalogOptionsFor(zai, profile), []catalog.Model{{ID: "glm-5.1", ContextLength: 200_000}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Remember(catalog.Options{Dir: profile}, []catalog.Model{{ID: "glm-5.1", ContextLength: 7}}); err != nil {
		t.Fatal(err)
	}
	if got := CachedContextWindow("z-ai/glm-5.1"); got != 200_000 {
		t.Fatalf("z-ai/glm-5.1 = %d, want its own service's 200000", got)
	}
}
