//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
)

func TestShellResolvesAuxiliaryModelsBeforeARealChildCallsTheProvider(t *testing.T) {
	for _, tc := range []struct {
		name, light string
		refused     bool
	}{
		{"floating-alias", "fixture/floating-latest", false},
		{"native-id", "fixture/native", false},
		{"unknown-id", "fixture/missing", true},
		{"omitted-light", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEAF_HOME", home)
			t.Setenv(config.ProfileDirEnv, home)
			t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-fixture")
			t.Setenv(carriedChildEnv, "model-flags")
			t.Setenv("DO_NOT_TRACK", "1")
			t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
			var mu sync.Mutex
			var sent []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
					rows := []any{}
					for _, id := range []string{"~fixture/floating-latest", "fixture/native"} {
						rows = append(rows, map[string]any{"id": id, "name": id, "context_length": 200000, "architecture": map[string]any{"input_modalities": []string{"text"}, "output_modalities": []string{"text"}}, "pricing": map[string]string{"prompt": "0", "completion": "0"}, "supported_parameters": []string{"tools", "tool_choice", "max_tokens"}})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
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
				mu.Lock()
				sent = append(sent, request.Model)
				mu.Unlock()
				if request.Model != "~fixture/floating-latest" && request.Model != "fixture/native" {
					http.Error(w, `{"error":{"message":"invalid model ID"}}`, 404)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2,\"cost\":0}}\n\ndata: [DONE]\n\n", request.Model)
			}))
			defer server.Close()
			t.Setenv("CODEAF_BASE_URL", server.URL+"/api/v1")
			oldCatalog, oldOut := sharedCatalog, carriedStdout
			sharedCatalog, carriedStdout = newSharedCatalog(), &lockedBuffer{}
			t.Cleanup(func() { sharedCatalog, carriedStdout = oldCatalog, oldOut })
			program := fakeModelFlagsProgram()
			restore := builtin.Override([]delegate.Delegate{program})
			defer restore()
			line := []string{"run", "--model", "fixture/floating-latest", "--dir", t.TempDir()}
			if tc.light != "" {
				line = append(line, "--light="+tc.light)
			}
			line = append(line, "--", "test both models")
			err := runCarried(program, line)
			mu.Lock()
			got := append([]string(nil), sent...)
			mu.Unlock()
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "fixture/missing") || len(got) != 0 {
					t.Fatalf("unknown light started calls or lacked its refusal: %v, %q", err, got)
				}
				return
			}
			wantLight := "~fixture/floating-latest"
			if tc.light == "fixture/native" {
				wantLight = tc.light
			}
			if exitCodeOf(err) != 0 || len(got) != 2 || got[0] != "~fixture/floating-latest" || got[1] != wantLight {
				t.Fatalf("model flags = %q, error = %v", got, err)
			}
		})
	}
}
