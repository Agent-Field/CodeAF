package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// C6: This directly exercises source refresh, not the attach trigger. A real
// process refreshes from another writer's profile, and both retained
// agents and the next real launch send the resolved key in actual HTTP requests.
func TestRunningProcessRefreshesConnectedKeyOnRealAgentRequests(t *testing.T) {
	for _, shellWins := range []bool{false, true} {
		t.Run(map[bool]string{false: "profile write", true: "shell still wins"}[shellWins], func(t *testing.T) {
			server := newV3ClientDoorServer(t)
			t.Setenv("CODEAF_BASE_URL", server.URL)
			proc := v3TestProcess(t)
			t.Setenv("OPENAI_API_KEY", "")
			if err := config.WriteAPIKey(proc.ProfileDir, "sk-or-v1-test-before"); err != nil {
				t.Fatal(err)
			}
			key := "sk-or-v1-test-before"
			t.Setenv(config.APIKeyEnv, "")
			if shellWins {
				key = "sk-or-v1-test-shell"
				t.Setenv(config.APIKeyEnv, key)
			}
			proc.Settings.BaseURL = server.URL
			if err := proc.setAPIKey(key); err != nil {
				t.Fatal(err)
			}
			proc.refreshModelSources()
			first, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			retained, err := session.New(first.Config)
			if err != nil {
				t.Fatal(err)
			}
			proc.track(retained)
			// This uses the same atomic profile writer as connect and the settings
			// row, with no in-process Applied hook to disguise a stale refresh.
			if err := config.WriteAPIKey(proc.ProfileDir, "sk-or-v1-test-after"); err != nil {
				t.Fatal(err)
			}
			proc.refreshModelSources()
			if !shellWins {
				key = "sk-or-v1-test-after"
			}
			next, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := session.New(next.Config)
			if err != nil {
				t.Fatal(err)
			}
			proc.track(fresh)
			for i, agent := range []*session.Agent{retained, fresh} {
				events, err := agent.Submit(t.Context(), "say done")
				if err != nil {
					t.Fatal(err)
				}
				for range events {
				}
				if got := server.call(t, i).authorization; got != "Bearer "+key {
					t.Fatalf("agent %d sent %q, want %q", i, got, "Bearer "+key)
				}
			}
			if !shellWins {
				if err := config.WriteAPIKey(proc.ProfileDir, ""); err != nil {
					t.Fatal(err)
				}
				proc.refreshModelSources()
				events, err := retained.Submit(t.Context(), "keep working")
				if err != nil {
					t.Fatal(err)
				}
				for range events {
				}
				if got := server.call(t, 2).authorization; got != "Bearer "+key {
					t.Fatalf("keyless profile revoked working key: %q", got)
				}
			}
		})
	}
}

// C6 and F2: Reopening or joining a conversation the real host still holds
// refreshes a real process and agent before the welcome. An independent profile
// file write, with no setter or model change in the engine, changes its next
// outgoing Authorization header; the shell's preferred key still wins.
func TestReattachingAHeldConversationRefreshesItsConnectedKey(t *testing.T) {
	for _, mode := range []string{"reopen", "join", "shell wins"} {
		t.Run(mode, func(t *testing.T) {
			server := newV3ClientDoorServer(t)
			t.Setenv("CODEAF_BASE_URL", server.URL)
			proc := v3TestProcess(t)
			shortEngineHome(t)
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv(config.APIKeyEnv, "")
			key := "sk-or-v1-test-held-before"
			if err := config.WriteAPIKey(proc.ProfileDir, key); err != nil {
				t.Fatal(err)
			}
			if mode == "shell wins" {
				key = "sk-or-v1-test-held-shell"
				t.Setenv(config.APIKeyEnv, key)
			}
			if err := proc.setAPIKey(key); err != nil {
				t.Fatal(err)
			}
			workspace := t.TempDir()
			file := filepath.Join(workspace, "held.jsonl")
			var boots atomic.Int32
			stopped := make(chan error, 1)
			go func() {
				stopped <- enginehost.Run(workspace, enginehost.Options{Boot: func(hello remote.Hello) (*remote.Engine, error) {
					boots.Add(1)
					launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: workspace, Session: file})
					if err != nil {
						return nil, err
					}
					agent, err := session.New(launch.Config)
					if err != nil {
						return nil, err
					}
					proc.track(agent)
					return &remote.Engine{Agent: agent, Workspace: workspace, SessionFile: file, RefreshModelSources: proc.refreshModelSources}, nil
				}})
			}()
			t.Cleanup(func() {
				_, _ = enginehost.Stop(workspace)
				select {
				case err := <-stopped:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(5 * time.Second):
					t.Error("key-refresh host did not stop")
				}
			})
			waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.PID == os.Getpid() })
			first := environmentWindow(t, workspace, file)
			instance := first.Welcome().SessionInstance
			send := func(client *remote.Client, expected string) {
				t.Helper()
				server.mu.Lock()
				index := len(server.calls)
				server.mu.Unlock()
				events, err := client.Agent().Submit(t.Context(), "say done")
				if err != nil {
					t.Fatal(err)
				}
				for range events {
				}
				// A journaled conversation can also ask for a title asynchronously.
				// Assert the actual turn's request, whose belt distinguishes it.
				server.mu.Lock()
				calls := append([]v3ClientDoorCall(nil), server.calls[index:]...)
				server.mu.Unlock()
				found := false
				for _, call := range calls {
					var tools []json.RawMessage
					_ = json.Unmarshal(call.body["tools"], &tools)
					if len(tools) == 0 {
						continue
					}
					found = true
					if got := call.authorization; got != "Bearer "+expected {
						t.Fatalf("reattached agent sent %q, want %q", got, "Bearer "+expected)
					}
				}
				if !found {
					t.Fatal("no real turn reached the local provider")
				}
			}
			send(first, key)
			if err := first.Agent().Detach(); err != nil {
				t.Fatal(err)
			}
			_ = first.Close()
			waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Surfaces == 0 && self.Conversations == 1 })
			// This disk write stands for another process's connect/settings write.
			// It never invokes proc.setAPIKey or proc.refreshModelSources.
			if err := config.WriteAPIKey(proc.ProfileDir, "sk-or-v1-test-held-after"); err != nil {
				t.Fatal(err)
			}
			if mode != "shell wins" {
				key = "sk-or-v1-test-held-after"
			}
			conn, err := enginehost.Dial(workspace)
			if err != nil {
				t.Fatal(err)
			}
			second, err := remote.Dial(conn, "returned-window", remote.Hello{Version: remote.Version, Workspace: workspace, Session: file, Join: mode == "join"})
			if err != nil {
				_ = conn.Close()
				t.Fatal(err)
			}
			defer second.Close()
			if boots.Load() != 1 || second.Welcome().SessionInstance != instance || second.Agent().Model() != "test/model" {
				t.Fatal("reattachment booted a new conversation or changed its model")
			}
			send(second, key)
		})
	}
}
