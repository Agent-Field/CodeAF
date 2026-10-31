//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// L4-A: The real provider-connected window remains joinable until a person
// sends a turn. No cache or startup store is invented by this fixture.
func TestL4ProviderWindowCanJoinUntilATurnIsSent(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}
	_ = binary(t)
	const model = "l4-window"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"context_length":32768}]}`, model)
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		send, ok := automationStream(w)
		if ok {
			send(`{"content":"L4 window answered."}`, "")
			send(`{}`, "stop")
			send("", "[DONE]")
		}
	})
	var listener net.Listener
	// The lane shares this laptop and reserves only these ports for its runs.
	var err error
	for port := 8900; port <= 8999; port++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: mux}}
	server.Start()
	t.Cleanup(server.Close)
	base := server.URL + "/v1"
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprintf("sent=%v", sent), func(t *testing.T) {
			login, err := os.MkdirTemp("/tmp", "rn-l4b-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(login) })
			root := filepath.Join(login, ".codeaf")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			// The native engine can outlive the window; stop this home's own
			// sockets before removing the home that makes them reachable.
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				sockets, err := filepath.Glob(filepath.Join(root, "v3", "engine-*.sock"))
				if err != nil {
					t.Error(err)
				}
				for _, socket := range sockets {
					if err := (cellstore.Daemon{Socket: socket}).Stop(ctx); err != nil {
						t.Error(err)
					}
				}
			})
			writeJSON(t, filepath.Join(root, "config.json"), map[string]any{
				"model.talk": model + "@local-models", "model.work": model + "@local-models",
			})
			// Connect through the same owner as `codeaf connect ollama`, with
			// a real listing probe and the persisted service/base selection.
			for _, source := range modelsource.Vendored() {
				if source.ID != modelsource.CustomID {
					continue
				}
				outcome, err := config.ConnectService(t.Context(), root, config.PersistedSource{
					ID: modelsource.CustomID, Written: "local-models", Address: base, KeyOptional: true, Order: 1,
				}, source, nil)
				if err != nil || outcome.Kind != modelsource.OutcomeConnected {
					t.Fatalf("connect local models: %+v, %v", outcome, err)
				}
			}
			// A minimal repository makes this a borrowed project without any
			// git mutation commands or a pre-created conversation.
			ws := filepath.Join(login, "project")
			for _, dir := range []string{"objects", "refs"} {
				if err := os.MkdirAll(filepath.Join(ws, ".git", dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(ws, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0600); err != nil {
				t.Fatal(err)
			}
			env := []string{"HOME=" + login, "CODEAF_BASE_URL=" + base, "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1"}
			for _, name := range append(append([]string{}, keylessEnv...), "CODEAF_BASE_URL", "CODEAF_MODEL", "CODEAF_RELAY", "CODEAF_SYNC_URL") {
				env = append([]string{"-u", name}, env...)
			}
			r := startWithEnv(t, env, fmt.Sprintf("l4_provider_%v", sent), root, ws, 120, 40, "chat", "--no-host")
			r.skipSetup(t)
			r.keys("Space", "Space")
			if _, ok := waitPlain(r, 20*time.Second, "[Home]"); !ok {
				t.Fatal("window did not reach Home")
			}
			cache := filepath.Join(root, "v3", "models-"+catalog.CacheKey(modelsource.CustomID, base)+".json")
			if !waitUntil(20*time.Second, func() bool { _, err := os.Stat(cache); return err == nil }) {
				t.Fatal("window did not write its keyed picker cache")
			}
			if sent {
				r.lit("Please say hello.")
				r.keys("Enter")
				if _, ok := waitPlain(r, 30*time.Second, "L4 window answered."); !ok {
					t.Fatal("scripted answer never reached the window")
				}
			}
			r.lit("/quit")
			r.keys("Enter")
			if !waitUntil(20*time.Second, func() bool {
				out, _ := exec.Command("tmux", "display-message", "-p", "-t", r.name, "#{pane_dead}").Output()
				return strings.TrimSpace(string(out)) == "1"
			}) {
				t.Fatal("window did not finish quitting")
			}
			r.kill()
			_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err == nil && !entry.IsDir() {
					rel, _ := filepath.Rel(root, path)
					t.Logf("window wrote %s", rel)
				}
				return err
			})
			joinable, err := identity.Joinable(root)
			if err != nil || joinable == sent {
				t.Errorf("Joinable = %v, %v; sent=%v", joinable, err, sent)
			}
			count, err := identity.ChatCount(root)
			want := 0
			if sent {
				want = 1
			}
			if err != nil || count != want {
				t.Errorf("ChatCount = %d, %v; want %d", count, err, want)
			}
		})
	}
}
