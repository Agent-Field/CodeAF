package run_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A real conversation and its real task engine must retain the selected
// account until the raw HTTP transport strips the provider's prefix.
func TestChatWorkerKeepsItsSelectedProviderModelAndCredential(t *testing.T) {
	t.Run("one model", func(t *testing.T) { chatWorkerProviderContract(t, true) })
	t.Run("routed crew", func(t *testing.T) { chatWorkerProviderContract(t, false) })
}

func chatWorkerProviderContract(t *testing.T, oneModel bool) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	t.Setenv("CODEAF_HOME", t.TempDir())
	var mu sync.Mutex
	type call struct{ endpoint, model, authorization string }
	var calls []call
	endpoint := func(name string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			var body struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode %s request: %v", name, err)
			}
			mu.Lock()
			calls = append(calls, call{name, body.Model, r.Header.Get("Authorization")})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"fixture account refused","code":403}}`))
		}))
		t.Cleanup(server.Close)
		return server
	}
	defaultServer, directServer := endpoint("default"), endpoint("direct")
	direct := modelsource.Source{ID: "direct", Written: "direct", Address: directServer.URL}
	place := session.Place{Dir: t.TempDir()}
	settings := session.Config{
		Workspace: t.TempDir(), Place: place, SessionFile: place.Transcript(),
		ProfileDir: t.TempDir(), Model: "direct/model", OneModel: oneModel,
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: modelsource.DefaultSource(defaultServer.URL), Address: defaultServer.URL, Key: "default-test-key"},
			modelsource.Connected{Source: direct, Address: directServer.URL, Key: "direct-test-key"},
		),
	}
	if !oneModel {
		settings.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) {
			return crewroute.Decision{Crew: []crewroute.Pick{
				{Seat: crewroute.Worker, Send: "direct/model"},
				{Seat: crewroute.Planner, Send: "direct/model"},
				{Seat: crewroute.Checker, Send: "direct/model"},
			}}, nil
		}
	}
	agent, err := session.New(settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	updates, stop := agent.WatchTaskUpdates()
	defer stop()
	id, _, _, err := agent.StartTask(t.Context(), "Write out.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-updates:
			if event.Task == nil || event.Task.ID != id || event.Task.State != session.TaskFailed {
				continue
			}
			mu.Lock()
			defer mu.Unlock()
			if len(calls) == 0 {
				t.Fatal("task ended without a provider call")
			}
			for _, got := range calls {
				if got != (call{"direct", "model", "Bearer direct-test-key"}) {
					t.Fatalf("chat task changed its selected account: %+v; all calls: %+v", got, calls)
				}
			}
			return
		case <-deadline.C:
			t.Fatal("real chat task did not finish after its provider refusal")
		}
	}
}
