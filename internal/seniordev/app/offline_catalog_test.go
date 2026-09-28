//go:build !windows

package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

func TestUnreachableModelsDevDoesNotRefuseCatalog(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("SENIOR_DEV_MODELS_URL", "http://127.0.0.1:1")
	t.Setenv("SENIOR_DEV_MODELS_PATH", "")
	t.Setenv("SENIOR_DEV_DISABLE_MODELS_FETCH", "")
	catalog, err := loadCatalog(context.Background(), io.Discard)
	if err != nil || catalog == nil {
		t.Fatalf("offline catalog = %#v, %v", catalog, err)
	}
}

func TestUnreachableModelsDevStillCallsLoopbackModelAPI(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("SENIOR_DEV_MODELS_URL", "http://127.0.0.1:1")
	t.Setenv("SENIOR_DEV_MODELS_PATH", "")
	t.Setenv("SENIOR_DEV_DISABLE_MODELS_FETCH", "")
	catalog, err := loadCatalog(context.Background(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer run-token" {
			t.Errorf("model API authorization = %q", request.Header.Get("Authorization"))
		}
		calls++
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, chatReply("done", 10))
	}))
	defer server.Close()
	backend := newModelAPIBackend(delegate.ModelAPI{BaseURL: server.URL, Token: "run-token"}, "")
	backend.catalog = catalog
	result, err := backend.Run(context.Background(), turn{
		Agent: "coder", ProviderID: "openrouter", ModelID: "vendor/offline-model",
		Prompt: "answer", Workspace: t.TempDir(), AgentMarkdown: testAgentPrompt,
	})
	if err != nil || result.Text != "done" || calls != 1 {
		t.Fatalf("loopback call: result=%+v calls=%d error=%v", result, calls, err)
	}
}
