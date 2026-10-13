package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppServeAgentLifecyclePersistsThroughRealCommand(t *testing.T) {
	root := t.TempDir()
	engineHome := filepath.Join(root, "engine")
	dir := filepath.Join(engineHome, "v3", "places", "app", "1234567890123456")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: "1234567890123456", Title: "Billing timeout", Model: "test/model", Workspace: root, Created: time.Now(), LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	launch := launchAppAtHomeForTest(t, root, false)
	response := launch.call(t, "GET", "/app/v1/agents?q=Billing", "", launch.hello.Token)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), "1234567890123456") {
		t.Fatalf("agents list %d: %s", response.StatusCode, body)
	}
	for _, action := range []string{"close", "archive", "reopen"} {
		response := launch.call(t, "POST", "/app/v1/agents/1234567890123456/"+action, `{}`, launch.hello.Token)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		var agent appapi.Agent
		if err := json.Unmarshal(body, &agent); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"close": "closed", "archive": "archived", "reopen": "idle"}[action]
		if response.StatusCode != 200 || agent.ID != "1234567890123456" || agent.State != want {
			t.Fatalf("%s: %d %s", action, response.StatusCode, body)
		}
	}
	response = launch.call(t, "POST", "/app/v1/shutdown", `{}`, launch.hello.Token)
	response.Body.Close()
	launch.wait(t)
	launch = launchAppAtHomeForTest(t, root, false)
	response = launch.call(t, "GET", "/app/v1/agents/1234567890123456", "", launch.hello.Token)
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), `"name":"Billing timeout"`) {
		t.Fatalf("restarted agent: %d %s", response.StatusCode, body)
	}
	response = launch.call(t, "POST", "/app/v1/shutdown", `{}`, launch.hello.Token)
	response.Body.Close()
	launch.wait(t)
	if _, err := os.Stat(transcript); err != nil {
		t.Fatal("lifecycle deleted transcript")
	}
}

func TestAppAgentCreationUsesInheritedMemoryKeyAndDeterministicName(t *testing.T) {
	provider := newV3ClientDoorServer(t)
	root := t.TempDir()
	const key = "opaque-a4a-memory-key-123456789"
	credentials := config.NewAppCredentials()
	credentials.Set("openrouter", key)
	defer config.UseAppCredentials(credentials)()
	opener := appAgentOpenerWithCommand(func(args ...string) *exec.Cmd {
		words := append([]string{"-test.run=^TestAppEngineKeysHelper$", "--"}, args[1:]...)
		command := exec.Command(os.Args[0], words...)
		command.Env = append(os.Environ(), "CODEAF_TEST_APP_ENGINE_CHILD=1", "CODEAF_TEST_APP_ENGINE_HOME="+root, "CODEAF_TEST_APP_ENGINE_URL="+provider.URL)
		return command
	}, io.Discard)
	registry := appapi.NewAgentRegistry(root, opener)
	defer registry.Close()
	api := appapi.New("app-agent-key-test-token-1234567890", appapi.Options{Agents: registry, Credentials: credentials, Epoch: "agent-key-test"})
	request, err := http.NewRequest("POST", "http://127.0.0.1:4000/app/v1/agents", strings.NewReader(`{"start":"send","text":"say done","model":"test/model"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer app-agent-key-test-token-1234567890")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != 201 {
		t.Fatalf("create did not use memory credentials: %d %s", response.Code, response.Body)
	}
	var agent appapi.Agent
	if err := json.Unmarshal(response.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.Name != "say done" || agent.ID == "" {
		t.Fatalf("wrong created agent: %+v", agent)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	endpoint := httptest.NewServer(api)
	defer endpoint.Close()
	events, err := http.NewRequestWithContext(ctx, "GET", endpoint.URL+"/app/v1/events?agent="+agent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	events.Header = request.Header.Clone()
	events.Header.Set("Last-Event-ID", "e_agent-key-test_0")
	responseEvents, err := endpoint.Client().Do(events)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(responseEvents.Body)
	ended := false
	for scanner.Scan() {
		if scanner.Text() == "event: turn.ended" {
			ended = true
			break
		}
	}
	_ = responseEvents.Body.Close()
	if !ended {
		t.Fatalf("agent did not finish: %v", scanner.Err())
	}
	if got := provider.call(t, 0).authorization; got != "Bearer "+key {
		t.Fatal("agent did not send its inherited app key")
	}
	read, _ := http.NewRequest("GET", "http://127.0.0.1:4000/app/v1/agents/"+agent.ID, nil)
	read.Header = request.Header.Clone()
	reply := httptest.NewRecorder()
	api.ServeHTTP(reply, read)
	var current appapi.Agent
	if err := json.Unmarshal(reply.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.State != "idle" {
		t.Fatalf("actual engine did not finish successfully: %s", reply.Body)
	}
	registry.Close()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if strings.Contains(string(raw), key) {
			t.Fatalf("agent saved the memory key in %s", path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
