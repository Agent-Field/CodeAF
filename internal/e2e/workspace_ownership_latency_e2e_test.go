//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// This compares idle screen response, not provider speed or large registries.
// A caller supplies the saved baseline binary; neither application is installed.
func TestWorkspaceOwnershipIdleTerminalLatency(t *testing.T) {
	baseline := os.Getenv("CODEAF_OWNERSHIP_BASELINE_BINARY")
	if baseline == "" {
		t.Skip("requires a saved workspace-ownership baseline binary")
	}
	for _, candidate := range []struct{ name, path string }{{"baseline", baseline}, {"candidate", binary(t)}} {
		t.Run(candidate.name, func(t *testing.T) {
			home := lifecycleHome(t, e2eModel)
			project := t.TempDir()
			var modelCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					modelCalls.Add(1)
					http.Error(w, "model calls forbidden in screen timing", http.StatusForbidden)
					return
				}
				if r.URL.Path == "/models" {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": e2eModel, "context_length": 131072}}})
					return
				}
				fmt.Fprint(w, `{"data":{"limit_remaining":100,"total_credits":100,"total_usage":0}}`)
			}))
			defer server.Close()
			env := []string{config.APIKeyEnv + "=fixture-latency", "CODEAF_BASE_URL=" + server.URL, "CODEAF_SYNC_URL=off", "CODEAF_PROFILE_DIR=", "CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1"}
			t.Cleanup(func() {
				binaries, _ := filepath.Glob(filepath.Join(home, "bin", "furrow-*"))
				for _, path := range binaries {
					lifecycleDaemonCleanup(t, home, path)
				}
			})
			stopRecoveryEngineOnCleanup(t, env, home, project)
			began := time.Now()
			rig := startUpgradeOldWindow(t, candidate.path, env, home, project)
			rig.skipSetup(t)
			// This includes the harness's launch/setup patience; it is kept out
			// of keyboard-to-screen samples and is not an app startup benchmark.
			results := map[string][]float64{"harness_launch_and_setup_ms": {float64(time.Since(began)) / float64(time.Millisecond)}}
			wait := func(name string, began time.Time, ready func(string) bool) {
				t.Helper()
				for time.Since(began) < 10*time.Second {
					if ready(rig.capture()) {
						results[name] = append(results[name], float64(time.Since(began))/float64(time.Millisecond))
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				t.Fatalf("%s did not render:\n%s", name, rig.capture())
			}
			send := func(keys ...string) {
				t.Helper()
				args := append([]string{"send-keys", "-t", rig.name}, keys...)
				if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
					t.Fatalf("keyboard delivery: %v %s", err, out)
				}
			}
			chat := func(screen string) bool { return strings.Contains(screen, "space space home") }
			for trial := 0; trial < 20; trial++ {
				began = time.Now()
				send("Space", "Space")
				wait("home_ms", began, func(screen string) bool { return strings.Contains(screen, "type to search or start something new") })
				began = time.Now()
				send("Escape")
				wait("home_return_ms", began, chat)
				send("-l", "/settings")
				wait("command_ms", time.Now(), func(screen string) bool { return strings.Contains(screen, "/settings") })
				began = time.Now()
				send("Enter")
				wait("settings_ms", began, func(screen string) bool {
					return strings.Contains(screen, "categories") && strings.Contains(screen, "type to search all")
				})
				began = time.Now()
				send("Escape")
				wait("settings_return_ms", began, chat)
			}
			if modelCalls.Load() != 0 {
				t.Fatalf("idle navigation made %d model calls", modelCalls.Load())
			}
			raw, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("actual terminal timing, 5ms polling plus command overhead:\n%s", raw)
			if evidence := os.Getenv("CODEAF_E2E_EVIDENCE_DIR"); evidence != "" {
				if err := os.WriteFile(filepath.Join(evidence, "ownership-latency-"+candidate.name+".json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
