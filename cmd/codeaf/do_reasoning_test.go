package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/router"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// R9.2 exercises the real task child settings, worker and provider adapter.
// A fresh service-owned cache must suffice without another discovery request.
func TestATaskChildSendsNoEffortToAModelWithoutReasoning(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "default service", true: "connected service"}[direct], func(t *testing.T) {
			for _, present := range noReasoningDoRequests(t, true, direct) {
				for _, key := range []string{"reasoning", "reasoning_effort", "thinking"} {
					if raw, ok := present[key]; ok {
						t.Errorf("task child sent unsupported %s: %s", key, raw)
					}
				}
			}
		})
	}
}

// R9's stability rule leaves plain do's explicit effort on the wire, even for
// the same model and cached catalog used by the task-child regression above.
func TestPlainCodeafDoStillSendsItsEffort(t *testing.T) {
	for _, present := range noReasoningDoRequests(t, false, false) {
		var reasoning struct {
			Effort string `json:"effort"`
		}
		if err := json.Unmarshal(present["reasoning"], &reasoning); err != nil || reasoning.Effort != "low" {
			t.Errorf("plain do changed its low effort: %s (%v)", present["reasoning"], err)
		}
	}
}

func noReasoningDoRequests(t *testing.T, taskChild, direct bool) []map[string]json.RawMessage {
	t.Helper()
	home := beltRunEnv(t)
	t.Setenv("HOME", home)
	model := "fixture/no-thinking"
	var mu sync.Mutex
	var calls []map[string]json.RawMessage
	listings := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			mu.Lock()
			listings++
			mu.Unlock()
			w.Write([]byte(`{"data":[{"id":"fixture/no-thinking","name":"No thinking","context_length":32768,"supported_parameters":["tools","max_tokens"],"pricing":{"prompt":"0.000001","completion":"0.000001"}},{"id":"no-thinking","supported_parameters":["tools","max_tokens"]}]}`))
		case "/chat/completions":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			mu.Lock()
			calls = append(calls, body)
			mu.Unlock()
			// Stop after the first real worker call; tool execution and task outcome
			// are independent of the request shape this endpoint is recording.
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"message":"fixture stops after recording the request","code":403}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	profile := config.ProfileDir()
	options := catalog.Options{BaseURL: server.URL, APIKey: "test-key", Dir: profile}
	if direct {
		if err := config.WriteSources(profile, []config.PersistedSource{{ID: "custom-fixture", Written: "local", Key: "test-key", Address: server.URL}}); err != nil {
			t.Fatal(err)
		}
		model = "local/no-thinking"
		settings, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		service, _ := settings.Sources.For(model)
		options = config.CatalogOptionsFor(service, profile)
	}
	models := catalog.Load(context.Background(), options)
	wireModel := model
	if direct {
		wireModel = "no-thinking"
	}
	if supported, known := models.SupportsParameter(wireModel, "reasoning"); supported || !known {
		t.Fatal("fixture did not publish a non-reasoning model")
	}
	// An unrelated default cache deliberately disagrees about this bare id.
	// The child must read the selected service's compartment, as chat does.
	if direct {
		if err := catalog.Remember(catalog.Options{BaseURL: server.URL, Dir: profile}, []catalog.Model{{ID: "no-thinking", Parameters: []string{"reasoning"}}}); err != nil {
			t.Fatal(err)
		}
	}
	// The provider panel owns a separate price cache. Prime it too, so any
	// discovery during do itself is observable instead of hidden by the fixture.
	router.LoadCatalog(profile, server.URL, "test-key", server.Client())
	before := listings
	oldCatalog := sharedCatalog
	sharedCatalog = newSharedCatalog()
	t.Cleanup(func() { sharedCatalog = oldCatalog })
	var stdout, stderr strings.Builder
	request := doRequest{task: "write out.txt", workspace: t.TempDir(), asJSON: true, model: model, planModel: model, checkModel: model,
		timeout: 30 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr}
	request.newClient = func(settings config.Config, model string) (*liveClient, error) {
		if got := settings.ClientConfig(model).DropRefusedEffort; got != taskChild {
			t.Errorf("refused-effort repair = %v, task child = %v", got, taskChild)
		}
		return newLiveClient(settings, model)
	}
	if taskChild {
		attempt := t.TempDir()
		request.runStore = filepath.Join(attempt, "run")
		raw, err := json.Marshal(taskdoor.Start{Effort: "low"})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(attempt, "request.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := doErrand(request); exitCodeOf(err) != 2 {
		t.Fatalf("expected fixture refusal, got %v\n%s\n%s", err, &stdout, &stderr)
	}
	// The accessor already belongs to do startup. Closing it joins any existing
	// warm-up before restoring the test seam and inspecting the request count.
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	sharedCatalog(settings).Close()
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("provider calls=%d; stdout=%s stderr=%s", len(calls), &stdout, &stderr)
	}
	if listings != before {
		t.Errorf("do fetched its catalog at startup: before %d, after %d", before, listings)
	}
	return calls
}
