package config

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestCachedSupportsParameterDistinguishesPublishedUnsupportedAndUnknownWithoutFetching(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	profile := t.TempDir()
	t.Setenv(ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	if err := catalog.Remember(catalog.Options{BaseURL: server.URL, Dir: profile}, []catalog.Model{
		{ID: "~vendor/latest", Parameters: []string{"response_format", "structured_outputs"}},
		{ID: "vendor/tools-only", Parameters: []string{"tools"}},
		{ID: "vendor/unpublished"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		model, parameter string
		supported, known bool
	}{
		{"vendor/latest", "response_format", true, true},
		{"openrouter/~vendor/latest", "structured_outputs", true, true},
		{"~vendor/latest:low", "structured_outputs", true, true},
		{"vendor/tools-only", "response_format", false, true},
		{"vendor/unpublished", "response_format", false, false},
		{"vendor/missing", "response_format", false, false},
		{"", "response_format", false, false},
		{"vendor/latest", "", false, false},
	} {
		if supported, known := CachedSupportsParameter(tc.model, tc.parameter); supported != tc.supported || known != tc.known {
			t.Errorf("%q %q = %v, %v; want %v, %v", tc.model, tc.parameter, supported, known, tc.supported, tc.known)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("a metadata lookup reached the network %d times", calls.Load())
	}
}

func TestCachedSupportsParameterUsesTheConnectedServicesOwnCompartment(t *testing.T) {
	profile := t.TempDir()
	t.Setenv(ProfileDirEnv, profile)
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_BASE_URL", "")
	if err := writeProfileValue(profile, keyModelSources, []PersistedSource{{ID: "z-ai", Written: "z-ai", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	var service modelsource.Connected
	for _, row := range ResolveSources(profile, "", "").All() {
		if row.Source.ID == "z-ai" {
			service = row
		}
	}
	if service.Source.ID == "" {
		t.Fatal("the service metadata did not resolve")
	}
	if err := catalog.Remember(CatalogOptionsFor(service, profile), []catalog.Model{{ID: "glm-5.1", Parameters: []string{"tools"}}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Remember(catalog.Options{Dir: profile}, []catalog.Model{{ID: "glm-5.1", Parameters: []string{"structured_outputs"}}}); err != nil {
		t.Fatal(err)
	}
	if supported, known := CachedSupportsParameter("z-ai/glm-5.1", "structured_outputs"); supported || !known {
		t.Fatalf("the other service borrowed default support: %v, %v", supported, known)
	}
}
