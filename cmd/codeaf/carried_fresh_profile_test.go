//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
)

// The real shell parser must let a model named by --high start on a fresh
// profile with only a provider key. The local server sees actual chat calls.
func TestSeniorDevExplicitShellModelRunsOnFreshProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("drives the real senior-dev child")
	}
	program, ok := builtin.Find("senior-dev")
	if !ok {
		t.Skip("senior-dev is unavailable in this build")
	}
	workspace := seniorDevWorkspace(t)
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-fixture")
	t.Setenv(carriedChildEnv, "real")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"fixture/vendor-model","canonical_slug":"fixture/vendor-model","name":"Fixture model","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0","request":"0"},"supported_parameters":["tools","tool_choice","max_tokens"]}]}`)
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if request.Model != "fixture/vendor-model" {
			http.Error(w, "wrong model: "+request.Model, 400)
			return
		}
		call := int(calls.Add(1))
		w.Header().Set("Content-Type", "text/event-stream")
		var tool, arguments string
		switch call {
		case 1:
			tool, arguments = "write", `{"filePath":"feature.txt","content":"implemented by stub\n"}`
		case 2:
			tool, arguments = "write", `{"filePath":".senior-dev/checklist.md","content":"- [x] feature implemented\n"}`
		case 3:
			tool, arguments = "submit", `{"reason":"feature implemented","evidence":"make test exit 0","checklist_satisfied":true}`
		}
		if tool != "" {
			argumentJSON, _ := json.Marshal(arguments)
			fmt.Fprintf(w, "data: {\"id\":\"fixture-%d\",\"model\":\"fixture/vendor-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-%d\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%s}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"cost\":0}}\n\ndata: [DONE]\n\n", call, call, tool, argumentJSON)
		} else {
			fmt.Fprintf(w, "data: {\"id\":\"fixture-%d\",\"model\":\"fixture/vendor-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"cost\":0}}\n\ndata: [DONE]\n\n", call)
		}
	}))
	defer server.Close()
	t.Setenv("CODEAF_BASE_URL", server.URL+"/api/v1")
	previous := carriedStdout
	output := &lockedBuffer{}
	carriedStdout = output
	t.Cleanup(func() { carriedStdout = previous })
	err := runCarried(program, []string{"run", "--high", "openrouter/fixture/vendor-model", "--dir", workspace, "--", "Add", "the", "feature."})
	if code := exitCodeOf(err); code != 0 || calls.Load() == 0 {
		t.Fatalf("fresh explicit shell run exited %d after %d chat calls: %s", code, calls.Load(), output.String())
	}
	if content, err := os.ReadFile(workspace + "/feature.txt"); err != nil || string(content) != "implemented by stub\n" {
		t.Fatalf("the shell did not make the feature: %q, %v", content, err)
	}
}

// With no explicit model, the shell still asks the profile for a work seat.
// A connected provider whose catalog offers none keeps the crew-door error.
func TestSeniorDevShellDefaultNamesCrewDoorWhenNoModelCanStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer server.Close()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-fixture")
	t.Setenv("CODEAF_BASE_URL", server.URL+"/api/v1")
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SetCrewAllowed(settings.ProfileDir, "no-such-model-anywhere"); err != nil {
		t.Fatal(err)
	}
	road, err := profileRoad()
	if err != nil {
		t.Fatal(err)
	}
	_, err = road.defaultSeat()
	if err == nil || !strings.Contains(err.Error(), "/crew") {
		t.Fatalf("empty profile default error = %v, want the crew door", err)
	}
}
