package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// TestAzureVariablesReachTheGatewayInItsOwnDialect drives the real
// ResolveSources → ClientConfigFor → provider.Client road from nothing but the
// LiteLLM-convention variables, against a stand-in that answers the way an
// Azure OpenAI gateway does: the v1 path, the `api-key` header, the deployment
// name as the body's model.
func TestAzureVariablesReachTheGatewayInItsOwnDialect(t *testing.T) {
	var seen map[string]any
	gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openai/v1/chat/completions" {
			t.Errorf("gateway reached %q, want the v1 chat path", request.URL.Path)
		}
		if request.Header.Get("api-key") != "azure-key" || request.Header.Get("Authorization") != "" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(request.Body).Decode(&seen)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"gpt-5.6-sol-2026-07-09","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	defer gateway.Close()
	t.Setenv(AzureBaseEnv, gateway.URL+"/")
	t.Setenv("AZURE_API_KEY", "azure-key")
	t.Setenv(AzureDeploymentEnv, "gpt-5.6-sol-2026-07-09")

	sources := ResolveSources(t.TempDir(), "", DefaultBaseURL)
	service, ok := sources.ByID(modelsource.AzureID)
	if !ok || service.Source.Preferred != "gpt-5.6-sol-2026-07-09" {
		t.Fatalf("azure service = %+v, %v; want it resolved from the environment", service, ok)
	}
	configured := ClientConfigFor(sources, "azure/gpt-5.6-sol-2026-07-09")
	if configured.Model != "gpt-5.6-sol-2026-07-09" || !configured.Direct || configured.KeyHeader != "api-key" || !configured.OpenAIDialect {
		t.Fatalf("configured client = %+v", configured)
	}
	client, err := provider.NewClient(configured)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}}}); err != nil {
		t.Fatal(err)
	}
	if seen["model"] != "gpt-5.6-sol-2026-07-09" {
		t.Fatalf("body model = %#v, want the bare deployment name", seen["model"])
	}
}

// TestAzureNeedsBothItsAddressAndItsKey keeps a half-set environment from
// putting a service on the list that every call would fail on.
func TestAzureNeedsBothItsAddressAndItsKey(t *testing.T) {
	t.Setenv(AzureBaseEnv, "https://gateway.example")
	t.Setenv("AZURE_API_KEY", "")
	if _, ok := ResolveSources(t.TempDir(), "", DefaultBaseURL).ByID(modelsource.AzureID); ok {
		t.Fatal("azure resolved with no key")
	}
	t.Setenv(AzureBaseEnv, "")
	t.Setenv("AZURE_API_KEY", "azure-key")
	if _, ok := ResolveSources(t.TempDir(), "", DefaultBaseURL).ByID(modelsource.AzureID); ok {
		t.Fatal("azure resolved with no address")
	}
}
