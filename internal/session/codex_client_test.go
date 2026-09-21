package session

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	account "github.com/Agent-Field/codeaf/internal/config"
)

func TestC13C14RealAgentUsesTheConfigOwnedCodexClientDoor(t *testing.T) {
	// C13: a real session.Agent reaches Codex through ResolveSources and ClientConfigFor.
	// C14: its ordinary streamed text and usage cross the same provider boundary as every direct service.
	var mutex sync.Mutex
	var path, authorization string
	var body map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		path, authorization = request.URL.Path, request.Header.Get("Authorization")
		_ = json.NewDecoder(request.Body).Decode(&body)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(writer, `data: {"type":"response.created","response":{"id":"agent-1","model":"gpt-5.5","created_at":1800000000}}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.output_text.delta","delta":"agent answer"}`)
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, `data: {"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":2}}}`)
		fmt.Fprintln(writer)
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	profile := t.TempDir()
	if err := codexauth.Save(profile, codexauth.Tokens{AccessToken: "agent-access-token", RefreshToken: "agent-refresh-token", IDToken: "agent-identity-token", AccountID: "agent-account", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := account.WriteSources(profile, []account.PersistedSource{{ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "codex/gpt-5.5", System: "Answer briefly.",
		Sources: account.ResolveSources(profile, "", account.DefaultBaseURL),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "hello from the agent")
	mutex.Lock()
	defer mutex.Unlock()
	encoded, _ := json.Marshal(body)
	if path != "/responses" || authorization != "Bearer agent-access-token" || !strings.Contains(string(encoded), "hello from the agent") {
		t.Fatalf("agent request path=%q authorization=%q body=%s", path, authorization, encoded)
	}
}
