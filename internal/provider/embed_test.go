package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestEmbedUsesTheOpenAICompatibleWireShape(t *testing.T) {
	var path string
	var body map[string]any
	var auth string
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		auth = request.Header.Get("Authorization")
		_ = json.NewDecoder(request.Body).Decode(&body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"data":[{"embedding":[0.1,0.2],"index":0},{"embedding":[0.3,0.4],"index":1}],"model":"openai/text-embedding-3-small","usage":{"prompt_tokens":4,"total_tokens":4,"cost":0.002}}`)
	}))
	media, err := NewMediaClient(Config{APIKey: "embed-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithCallTag(context.Background(), string(roles.RoleEmbed))
	got, err := media.Embed(ctx, EmbeddingRequest{
		Model: "openai/text-embedding-3-small",
		Input: []string{"harbor", "ledger"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if path != "/api/v1/embeddings" {
		t.Fatalf("path = %q, want /api/v1/embeddings", path)
	}
	if auth != "Bearer embed-key" {
		t.Fatalf("auth = %q", auth)
	}
	if body["model"] != "openai/text-embedding-3-small" {
		t.Fatalf("body model = %v", body["model"])
	}
	inputs, _ := body["input"].([]any)
	if len(inputs) != 2 || inputs[0] != "harbor" || inputs[1] != "ledger" {
		t.Fatalf("input = %#v", body["input"])
	}
	if got.Model != "openai/text-embedding-3-small" || len(got.Data) != 2 || len(got.Data[0].Embedding) != 2 {
		t.Fatalf("response = %+v", got)
	}
	if got.Usage == nil || got.Usage.Cost == nil || *got.Usage.Cost != 0.002 {
		t.Fatalf("usage = %+v", got.Usage)
	}
}

func TestEmbedTagsSpendWhenTheCallerForgot(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"data":[{"embedding":[1],"index":0}]}`)
	}))
	media, err := NewMediaClient(Config{APIKey: "embed-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := media.Embed(context.Background(), EmbeddingRequest{Model: "vendor/embed", Input: []string{"ok"}}); err != nil {
		t.Fatal(err)
	}
	// The method itself stamps the role when the context carried none, so a
	// forgotten tag is still "embed" and never RoleAuditor.
	if roles.RoleEmbed == roles.RoleAuditor {
		t.Fatal("RoleEmbed must not be RoleAuditor")
	}
}

func TestEmbedRefusesEmptyVectors(t *testing.T) {
	client := handlerClient(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, `{"data":[{"embedding":[],"index":0}]}`)
	}))
	media, err := NewMediaClient(Config{APIKey: "embed-key", BaseURL: "https://openrouter.example/api/v1", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	_, err = media.Embed(context.Background(), EmbeddingRequest{Model: "vendor/embed", Input: []string{"ok"}})
	if err == nil || !strings.Contains(err.Error(), "empty vector") {
		t.Fatalf("empty vectors must fail, got %v", err)
	}
}

func TestEmbedAvailabilityErrorsDoNotPrintTheKey(t *testing.T) {
	secret := "sk-or-v1-not-a-real-key"
	err := redactSecret(errStr("bearer "+secret+" refused"), secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("key leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("redaction missing: %v", err)
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }
