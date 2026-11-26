package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppPolicyAtProviderSend(t *testing.T) {
	var calls, redirected atomic.Int32
	var redirect atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/unapproved" {
			redirected.Add(1)
		}
		calls.Add(1)
		if redirect.Load() && r.URL.Path != "/unapproved" {
			w.Header().Set("Location", "/unapproved")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test/model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)
	profile := t.TempDir()
	policy := appModelPolicy{profile: profile, classes: func(context.Context) ([]schema.DataClass, error) {
		return []schema.DataClass{schema.ClassRestricted}, nil
	}, audit: func(string) error { return nil }}
	client, err := provider.NewClient(provider.Config{
		Model: "test/model", APIKey: "fake", BaseURL: server.URL + "/v1", Direct: true, AuthorizeDestination: policy.authorize,
	})
	if err != nil {
		t.Fatal(err)
	}
	paths := []struct {
		name string
		call func() error
	}{
		{"run step", func() error { _, err := runs.CompleteAgent(context.Background(), client, "app", "private"); return err }},
		{"builder", func() error {
			_, err := session.ScopedCommandTurn(context.Background(), t.TempDir(), "test/model", "app", "private", builderModelClient{client: client, model: "test/model"}, appPolicyNoCommand)
			return err
		}},
		{"agent", func() error {
			_, err := session.ScopedCommandTurn(context.Background(), t.TempDir(), "test/model", "app", "private", client, appPolicyNoCommand)
			return err
		}},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			if err := path.call(); !errors.Is(err, errAppModelRestricted) {
				t.Fatalf("refusal: %v", err)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("unapproved model received %d calls", got)
			}
		})
	}
	raw := []byte(`{"restricted":[{"model":"test/model","endpoint":"` + server.URL + `/v1/chat/completions"}]}`)
	if err := writeAppModelPolicy(filepath.Join(profile, appModelPolicyFile), raw); err != nil {
		t.Fatal(err)
	}
	if err := paths[0].call(); err != nil || calls.Load() != 1 {
		t.Fatalf("approved run calls=%d: %v", calls.Load(), err)
	}
	redirect.Store(true)
	if err := paths[0].call(); err == nil || redirected.Load() != 0 {
		t.Fatalf("an approved model redirected to an unapproved path: %v", err)
	}
}

func appPolicyNoCommand(context.Context, string) (string, error) {
	return "", errors.New("no command in this test")
}

func TestAppPolicyProductionModelPaths(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	var delivered atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			delivered.Add(1)
		}
		http.Error(w, "must stop before sending", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_BASE_URL", server.URL+"/v1")
	t.Setenv("OPENROUTER_API_KEY", "test-model-key")
	t.Setenv("CODEAF_APPS_BUILDER_MODEL", "test/model")
	definitions := store.New()
	draft, err := definitions.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/store/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(draft.Dir(), "app.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Patch the declaration without re-encoding the legacy app fixture.
	var properties map[string]any
	if err := json.Unmarshal(raw, &properties); err != nil {
		t.Fatal(err)
	}
	properties["records"].(map[string]any)["deal"].(map[string]any)["fields"].(map[string]any)["name"].(map[string]any)["class"] = "restricted"
	properties["agent"].(map[string]any)["model"] = "test/model"
	raw, err = json.Marshal(properties)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	meta, err := definitions.Promote(draft, "Keep this app")
	if err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	b := newFakeBackend(rec, nil)
	t.Cleanup(func() { b.Close() })
	run := b.appRunAgent(runs.Files(home.Join("apps")))
	for _, id := range []string{"model-step", appAgentAskRun} {
		_, err := run(t.Context(), runs.AgentCall{Call: runs.Call{App: meta.ID, Run: id, ID: "test-run"}, Prompt: "Summarize deals", Remaining: 1})
		if !errors.Is(err, errAppModelRestricted) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	harnesses := b.appAIHarnesses(runs.Files(home.Join("apps")))
	for _, id := range []string{runs.HarnessClaudeCode, runs.HarnessCodex, runs.HarnessAgentField} {
		_, err := harnesses[id].Run(t.Context(), runs.AgentCall{Call: runs.Call{App: meta.ID}, Prompt: "Summarize deals"})
		if !errors.Is(err, errAppModelRestricted) {
			t.Fatalf("harness %s: %v", id, err)
		}
	}
	jobs, err := b.AppBuilds()
	if err != nil {
		t.Fatal(err)
	}
	edit, err := jobs.Change(meta.ID, "Change the deal heading")
	if err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, payload any) bool {
		out, ok := payload.(builder.Event)
		if event != appwire.EventAppsBuild || !ok || out.Draft != edit {
			return false
		}
		if out.Stage == builder.Failed && !strings.Contains(out.Line, errAppModelRestricted.Error()) {
			t.Fatalf("builder failure: %s", out.Line)
		}
		return strings.Contains(out.Line, errAppModelRestricted.Error())
	})
	if got := delivered.Load(); got != 0 {
		t.Fatalf("the unapproved endpoint received %d requests", got)
	}
}
