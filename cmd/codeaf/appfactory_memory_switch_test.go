package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestMemorySwitchDefaultsOffAndChangesLive(t *testing.T) {
	t.Run("fresh app job and chat", freshMemoryOffApp)
	t.Run("saved words and booleans", func(t *testing.T) {
		for _, value := range []any{"on", "off", true, false} {
			profile := v3Profile(t, map[string]any{config.KeyMemoryEnabled: value})
			want := value == "on" || value == true
			got, saved := config.MemorySettingAt(profile)
			if got != want || !saved || knowledge.Open(profile).Enabled() != want {
				t.Fatalf("saved %v = (%v,%v), want (%v,true)", value, got, saved, want)
			}
		}
	})
	t.Run("legacy consent without saved choice", func(t *testing.T) {
		profile := t.TempDir()
		vault := knowledge.Open(profile)
		if err := vault.StartBuild(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.BudgetConfigPath(profile), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		state, err := vault.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if vault.Enabled() || state.Enabled {
			t.Fatal("legacy vault consent enabled memory without a saved choice")
		}
	})
	s := newLocalSetup(t)
	s.backend.emit = s.server
	profile := config.ProfileDir()
	vault := knowledge.Open(profile)
	if config.MemoryEnabledAt(profile) || vault.Enabled() {
		t.Error("fresh home enables automatic memory without consent")
	}
	read := func(method string, params any, enabled bool, source string) {
		t.Helper()
		var reply struct {
			Settings struct {
				Memory *struct {
					Enabled  bool   `json:"enabled"`
					Source   string `json:"source"`
					CostNote string `json:"cost_note"`
				} `json:"memory"`
			} `json:"settings"`
		}
		s.must(method, params, &reply)
		m := reply.Settings.Memory
		if m == nil || m.Enabled != enabled || m.Source != source || m.CostNote != config.MemoryCostNote {
			t.Fatalf("%s memory = %+v, want enabled=%v source=%s and cost disclosure", method, m, enabled, source)
		}
	}
	read("settings.get", struct{}{}, false, "default")
	if !strings.Contains(s.hello, "settings.memory") {
		t.Error("hello omits settings.memory")
	}
	read("settings.set", map[string]any{"memory_enabled": true}, true, "saved")
	if !config.MemoryEnabledAt(profile) || !vault.Enabled() {
		t.Fatal("on did not reach the existing vault")
	}
	read("settings.set", map[string]any{"memory_enabled": false}, false, "saved")
	if config.MemoryEnabledAt(profile) || vault.Enabled() {
		t.Fatal("off did not reach the existing vault")
	}
	for _, bad := range []any{"on", 1, nil} {
		s.mustFail("settings.set", map[string]any{"memory_enabled": bad}, appwire.CodeBadParams)
	}
	if err := vault.Flush(context.Background(), "fixture", false); err != knowledge.ErrDisabled {
		t.Fatalf("off compile = %v, want disabled", err)
	}
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	read("settings.get", struct{}{}, true, "saved")
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, line := range s.lines {
		if strings.Contains(line, "settings.changed") && strings.Contains(line, `"memory"`) {
			found = true
		}
	}
	if !found {
		t.Error("settings.changed omits memory")
	}
}

func freshMemoryOffApp(t *testing.T) {
	s := newLocalSetup(t)
	var posts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		posts.Add(1)
		var body struct {
			Tools []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		delta := map[string]any{"content": "Cedar queues jobs."}
		finish := "stop"
		for _, tool := range body.Tools {
			if tool.Function.Name == "stage_result" {
				delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "report", "type": "function", "function": map[string]any{"name": "stage_result", "arguments": `{"done":true,"output":"Cedar queues jobs."}`}}}}
				finish = "tool_calls"
			}
		}
		raw, _ := json.Marshal(map[string]any{"model": knowledge.Model, "choices": []any{map[string]any{"delta": delta, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .01}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
	}))
	t.Cleanup(server.Close)
	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		if strings.Contains(u.Host, "openrouter.ai") {
			clone.URL.Scheme = "http"
			clone.URL.Host = server.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(clone)
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })
	profile := v3Profile(t, map[string]any{config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: knowledge.Model, "models.tiers.low": knowledge.Model, "models.tiers.reflex": knowledge.Model, "models.roles": ""})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", "fixture")
	t.Setenv("CODEAF_BASE_URL", server.URL)
	t.Setenv("CODEAF_MODEL_POOL", "off")
	callPath := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv(calllog.EnvVar, callPath)
	freshEngineProcess(t)
	g := newRunRig(t)
	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Model: knowledge.Model, OneModel: true, Workspace: g.workspace})
	if err != nil {
		t.Fatal(err)
	}
	stageSettle = 20 * time.Millisecond
	t.Cleanup(func() { stageSettle = unattendedRunSettleTick })
	s.backend.open = appOpenInProcess
	local := &s.backend.local
	local.parent = func() session.Config { return launch.Config }
	local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}
	st := s.start()
	floor := local.host.floor
	if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
		t.Fatal(err)
	}
	id, err := floor.Add(context.Background(), factory.Item{Repo: "acme/api", Title: "Cedar job", Cap: 1, Tier: factory.TierOwner, Model: knowledge.Model, Stages: []factory.Stage{{Name: "work", Kind: factory.StageChat, On: true, Until: "done", Ask: "Call stage_result with done true and output Cedar queues jobs."}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true}}})
	if err != nil {
		t.Fatal(err)
	}
	var job api.Job
	s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
	s.must("factory.job_start", map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}, &appwire.FactoryJobActed{})
	deadline := time.Now().Add(15 * time.Second)
	for {
		it, err := floor.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if it.State == factory.StateLanded {
			break
		}
		if it.Failure != nil || it.Question != "" || time.Now().After(deadline) {
			t.Fatalf("job did not finish: %+v", it)
		}
		time.Sleep(10 * time.Millisecond)
	}
	agent, err := session.New(launch.Config)
	if err != nil {
		t.Fatal(err)
	}
	events, err := agent.Submit(context.Background(), "Cedar queues my jobs.")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	t.Cleanup(func() { _ = agent.Close() })
	raw, err := os.ReadFile(callPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		var row calllog.Record
		if json.Unmarshal([]byte(line), &row) == nil && (row.Tag == "memory" || row.Tag == "reflex") {
			t.Errorf("fresh app made memory call: %s", line)
		}
	}
	if posts.Load() < 2 {
		t.Fatalf("job and chat did not call stub: %d", posts.Load())
	}
	vault := knowledge.Open(profile)
	if vault.Enabled() || !vault.EnabledSince().IsZero() {
		t.Error("background worker armed fresh memory or wrote enabled consent")
	}
	s.must("settings.set", map[string]any{"memory_enabled": true}, nil)
	events, err = agent.Submit(context.Background(), "Remember that I prefer Cedar queues.")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		raw, _ = os.ReadFile(callPath)
		if strings.Contains(string(raw), `"tag":"reflex"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("wire on did not start memory in existing chat")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.must("settings.set", map[string]any{"memory_enabled": false}, nil)
	// Let the already-started request finish before measuring the next turn.
	time.Sleep(50 * time.Millisecond)
	raw, err = os.ReadFile(callPath)
	if err != nil {
		t.Fatal(err)
	}
	before := strings.Count(string(raw), `"phase":"start","tag":"reflex"`)
	events, err = agent.Submit(context.Background(), "Explain Cedar again.")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(callPath)
	if after := strings.Count(string(raw), `"phase":"start","tag":"reflex"`); after != before {
		t.Fatalf("wire off admitted another memory request: before=%d after=%d", before, after)
	}

}

// The switch makes off a real state, so a store that can't open must say why
// and never read as the person's choice.
func TestMemoryStoreFailureNamesItsReasonNotOff(t *testing.T) {
	b := &appBackend{memoryStore: func() (*store.Store, error) { return nil, errors.New("no provider is set up") }}
	_, err := b.memoryOf()
	if err == nil || strings.Contains(err.Error(), "memory is off") || !strings.Contains(err.Error(), "no provider is set up") {
		t.Fatalf("memoryOf error = %v", err)
	}
}

// With no key at launch the memory list speaks to the person who has to add
// one, and names where. The variable name is for a developer's terminal.
func TestMemoryStoreWithNoKeySaysWhereToAddOne(t *testing.T) {
	for _, cause := range []error{
		config.ErrNoAPIKey,
		fmt.Errorf("open memory: %w", config.ErrNoAPIKey),
		errors.New(config.ErrNoAPIKey.Error()),
	} {
		b := &appBackend{memoryStore: func() (*store.Store, error) { return nil, cause }}
		_, err := b.memoryOf()
		if err == nil || strings.Contains(err.Error(), config.APIKeyEnv) || !strings.Contains(err.Error(), "Settings › Accounts › Providers") {
			t.Fatalf("memoryOf(%v) = %v", cause, err)
		}
	}
}
