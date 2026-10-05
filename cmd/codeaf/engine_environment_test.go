package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// C1, C2 and C3: The helper runs the real host in a separate process, with real
// journaled agents. Arguments restore the fake test key after TestMain's safety
// floor, so no inherited live credential can ever be used by the child.
func TestEngineEnvironmentHostHelper(t *testing.T) {
	if flag.NArg() != 5 || flag.Arg(0) != "environment-host" {
		t.Skip("only the environment contract starts this child")
	}
	workspace, key := flag.Arg(1), flag.Arg(2)
	t.Setenv("CODEAF_HOME", flag.Arg(3))
	t.Setenv(config.APIKeyEnv, key)
	t.Setenv(config.ProfileDirEnv, flag.Arg(4))
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	// F1: Keep the grace unmistakably live without making a test wait for it.
	remote.WatchFor = time.Hour
	err := enginehost.Run(workspace, enginehost.Options{Boot: func(hello remote.Hello) (*remote.Engine, error) {
		file := hello.Session
		if file == "" {
			file = filepath.Join(workspace, "conversation.jsonl")
		}
		_, statErr := os.Stat(file)
		agent, err := session.New(session.Config{Workspace: workspace, SessionFile: file, Model: "test/model", APIKey: key, BaseURL: env.Get("CODEAF_BASE_URL")})
		if err != nil {
			return nil, err
		}
		return &remote.Engine{Agent: agent, Workspace: workspace, SessionFile: file, Resumed: statErr == nil}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
}

// environmentChild starts the same test binary as a host and joins it on
// cleanup. The socket handshake, rather than a timing guess, establishes birth.
func environmentChild(t *testing.T, workspace, key string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestEngineEnvironmentHostHelper$", "-test.v", "--", "environment-host", workspace, key, os.Getenv("CODEAF_HOME"), os.Getenv(config.ProfileDirEnv))
	// The test-binary state guard quarantines the root inherited at birth.
	// Choose the real fixture after init, exactly as the in-process tests do.
	cmd.Env = env.EnvironWithout("CODEAF_HOME")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		// Stop our own host normally so every journal and background job closes.
		if self, err := enginehost.Ask(workspace, remote.WhoIs{}); err == nil && self.PID == cmd.Process.Pid {
			_, _ = enginehost.Stop(workspace)
		}
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("child host did not exit")
		}
	})
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.PID == cmd.Process.Pid })
	return cmd
}

func waitEnvironmentHost(t *testing.T, workspace string, accept func(remote.HostSelf) bool) remote.HostSelf {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		self, err := enginehost.Ask(workspace, remote.WhoIs{})
		if err == nil && accept(self) {
			return self
		}
		if time.Now().After(deadline) {
			t.Fatalf("host never reached the requested state: self %+v, error %v", self, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func environmentWindow(t *testing.T, workspace, file string) *remote.Client {
	t.Helper()
	conn, err := enginehost.Dial(workspace)
	if err != nil {
		t.Fatal(err)
	}
	client, err := remote.Dial(conn, "environment-test", remote.Hello{Version: remote.Version, Workspace: workspace, Session: file})
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func environmentTestHome(t *testing.T) (string, *v3ClientDoorServer) {
	t.Helper()
	shortEngineHome(t)
	t.Setenv(config.ProfileDirEnv, os.Getenv("CODEAF_HOME"))
	t.Setenv("OPENAI_API_KEY", "")
	server := newV3ClientDoorServer(t)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-host-before")
	return t.TempDir(), server
}

// C1 and F1: Immediately after windows act and disconnect, the ordinary watch
// grace still reads busy, but environment retirement produces a different PID,
// sends the new key, and reopens every retained conversation from its journal.
func TestDifferentEnvironmentRestartsIdleRealHostAndReopensJournals(t *testing.T) {
	workspace, server := environmentTestHome(t)
	old := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	files := []string{filepath.Join(workspace, "one.jsonl"), filepath.Join(workspace, "two.jsonl")}
	for i, file := range files {
		client := environmentWindow(t, workspace, file)
		events, err := client.Agent().Submit(t.Context(), fmt.Sprintf("keep this conversation %d", i))
		if err != nil {
			t.Fatal(err)
		}
		for range events {
		}
		// Closing the pipe, as a terminal exit can do, retains the watch grace.
		_ = client.Close()
	}
	held := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Surfaces == 0 && self.Conversations == len(files) })
	if !held.Busy {
		t.Fatal("the fixture did not exercise the recently acted watch grace")
	}
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-host-after")
	note, err := clearStaleEngineHost(workspace)
	if err != nil || !strings.Contains(note, "restarted to pick up this terminal's environment") {
		t.Fatalf("takeover: %q, %v", note, err)
	}
	var replacement *exec.Cmd
	conn, err := enginehost.Attach(workspace, func() error { replacement = environmentChild(t, workspace, os.Getenv(config.APIKeyEnv)); return nil })
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	self := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return !self.Busy })
	if replacement == nil || self.PID == old.Process.Pid || self.PID != replacement.Process.Pid {
		t.Fatalf("host PID %d did not replace %d", self.PID, old.Process.Pid)
	}
	if enginehost.EnvironmentDiffers(workspace) {
		t.Fatal("replacement kept the old environment")
	}
	server.mu.Lock()
	requestIndex := len(server.calls)
	server.mu.Unlock()
	for i, file := range files {
		client := environmentWindow(t, workspace, file)
		if !client.Welcome().Resumed {
			t.Fatal("conversation was not resumed")
		}
		transcript := fmt.Sprint(client.Agent().Transcript())
		if !strings.Contains(transcript, fmt.Sprintf("keep this conversation %d", i)) || !strings.Contains(transcript, "done") {
			t.Fatalf("lost journal: %s", transcript)
		}
		events, err := client.Agent().Submit(t.Context(), "use the new environment")
		if err != nil {
			t.Fatal(err)
		}
		for range events {
		}
		if got := server.call(t, requestIndex).authorization; got != "Bearer sk-or-v1-test-host-after" {
			t.Fatalf("replacement sent %q", got)
		}
		requestIndex++
	}
}

// C2: A real host with an attached window refuses environment retirement. The
// new window joins the same PID, and the note names the host's exact workspace.
func TestDifferentEnvironmentJoinsBusyRealHostWithoutStoppingIt(t *testing.T) {
	workspace, _ := environmentTestHome(t)
	old := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	first := environmentWindow(t, workspace, "")
	before := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Busy })
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-different-terminal")
	note, err := clearStaleEngineHost(workspace)
	if err != nil || note != differentEngineEnvironmentSentence(before.Workspace) {
		t.Fatalf("busy note: %q, %v", note, err)
	}
	conn, err := enginehost.Attach(workspace, func() error { t.Error("busy host caused a spawn"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	second, err := remote.Dial(conn, "second-window", remote.Hello{Version: remote.Version, Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Surfaces == 2 })
	if before.PID != after.PID || after.PID != old.Process.Pid {
		t.Fatalf("busy host changed PID: %d -> %d", before.PID, after.PID)
	}
	if first.Agent().Model() != second.Agent().Model() {
		t.Fatal("new window failed to join the existing conversation")
	}
}

// C3 and C4: Real same-build hosts keep their PID with equal environments,
// irrelevant shell changes, or a missing fingerprint, with no launch note.
func TestSameOrUnknownEnvironmentJoinsRealHostSilently(t *testing.T) {
	for _, mode := range []string{"same", "shell bookkeeping", "missing fingerprint"} {
		t.Run(mode, func(t *testing.T) {
			workspace, _ := environmentTestHome(t)
			old := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
			if mode == "shell bookkeeping" {
				for _, name := range []string{"PWD", "TERM", "PATH", "SHLVL", "TMUX"} {
					t.Setenv(name, "irrelevant-test-value")
				}
			}
			if mode == "missing fingerprint" {
				dir, err := enginehost.Dir(workspace)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(dir, "environment")); err != nil {
					t.Fatal(err)
				}
				t.Setenv(config.APIKeyEnv, "sk-or-v1-test-different")
			}
			note, err := clearStaleEngineHost(workspace)
			if err != nil || note != "" {
				t.Fatalf("unchanged or unknown environment: %q, %v", note, err)
			}
			conn, err := enginehost.Attach(workspace, func() error { t.Error("existing host caused a spawn"); return nil })
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			self := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Busy })
			if self.PID != old.Process.Pid {
				t.Fatalf("existing host changed PID: %d -> %d", old.Process.Pid, self.PID)
			}
		})
	}
}

// C13: Both real status commands print one environment line only for a known
// difference, leaving equal and unknown readings absent under the emptiness law.
func TestEngineStatusReportsOnlyKnownEnvironmentDifferences(t *testing.T) {
	workspace, _ := environmentTestHome(t)
	environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	for _, mode := range []string{"same", "different", "unknown"} {
		if mode == "different" {
			t.Setenv(config.APIKeyEnv, "sk-or-v1-test-status-change")
		}
		if mode == "unknown" {
			dir, _ := enginehost.Dir(workspace)
			if err := os.Remove(filepath.Join(dir, "environment")); err != nil {
				t.Fatal(err)
			}
		}
		for _, show := range []func(*strings.Builder) error{
			func(out *strings.Builder) error { return runEngineStatus(out, workspace) },
			func(out *strings.Builder) error { return runEngineStatusAll(out) },
		} {
			var output strings.Builder
			if err := show(&output); err != nil {
				t.Fatal(err)
			}
			count := strings.Count(output.String(), "  environment differs from this terminal's")
			want := 0
			if mode == "different" {
				want = 1
			}
			if count != want {
				t.Fatalf("%s status: %s", mode, output.String())
			}
		}
	}
}

// C2: A real turn continues with no window attached. An environment change
// cannot stop it, and the arriving window joins that same host process.
func TestDifferentEnvironmentKeepsAnUnattachedRealTurnRunning(t *testing.T) {
	workspace, _ := environmentTestHome(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n"+"data: [DONE]\n\n")
	}))
	defer server.Close()
	defer close(release)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	old := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	first := environmentWindow(t, workspace, "")
	if _, err := first.Agent().Submit(t.Context(), "keep running"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never reached the local provider")
	}
	if err := first.Agent().Detach(); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Surfaces == 0 && self.Busy })
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-other-terminal")
	note, err := clearStaleEngineHost(workspace)
	if err != nil || note != differentEngineEnvironmentSentence(workspace) {
		t.Fatalf("running-turn note: %q, %v", note, err)
	}
	second := environmentWindow(t, workspace, "")
	if !second.Welcome().Persistent {
		t.Fatal("window did not join the running host")
	}
	self := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Busy })
	if self.PID != old.Process.Pid {
		t.Fatal("environment change stopped the unattached turn")
	}
}

// C3 and D1: The existing build-order rule still joins a newer real host even
// when that host's recorded environment differs from the older window's.
func TestOlderWindowJoinsNewerRealHostDespiteDifferentEnvironment(t *testing.T) {
	workspace, _ := environmentTestHome(t)
	old := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	held := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return !self.Busy })
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-other-terminal")
	note, err := clearStaleEngineHostAs(workspace, window("older-build", held.BuiltAt.Add(-time.Hour)))
	if err != nil || note != "" {
		t.Fatalf("older window disturbed newer host: %q, %v", note, err)
	}
	if self := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return !self.Busy }); self.PID != old.Process.Pid {
		t.Fatal("newer host changed PID")
	}
}

// C3: A real daemon changes into its workspace before Run. A relative profile's
// custom key references must resolve there on both sides, so identical process
// environments still join that host with the same PID and no restart note.
func TestRelativeProfileDoesNotRestartAnEqualEnvironmentHost(t *testing.T) {
	workspace, _ := environmentTestHome(t)
	profile := "spec-relative-profile"
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("MY_SPEC_PROVIDER_KEY", "test-custom-provider-secret")
	if err := config.WriteSources(filepath.Join(workspace, profile), []config.PersistedSource{{ID: "z-ai", Written: "z-ai", KeyEnv: "MY_SPEC_PROVIDER_KEY"}}); err != nil {
		t.Fatal(err)
	}
	old := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	if enginehost.EnvironmentDiffers(workspace) {
		t.Fatal("the daemon chdir changed the canonical profile key references")
	}
	note, err := clearStaleEngineHost(workspace)
	if err != nil || note != "" {
		t.Fatalf("equal environment restarted its host: %q, %v", note, err)
	}
	if self := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return !self.Busy }); self.PID != old.Process.Pid {
		t.Fatal("relative profile changed the host PID")
	}
}

// C2 and F1: A real agent's background job outlives its turn and its window.
// Ignoring the watch grace must still keep that job and its engine PID alive.
func TestDifferentEnvironmentKeepsARealHandedOffJobRunning(t *testing.T) {
	workspace, _ := environmentTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools    []json.RawMessage `json:"tools"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"job"},"finish_reason":"stop"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if len(body.Tools) > 0 && len(body.Messages) > 0 && body.Messages[len(body.Messages)-1].Role == "user" {
			// The FIFO holds the real job without a clock or a long real sleep.
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"start-job","type":"function","function":{"name":"bash","arguments":"{\"command\":\"mkfifo job-gate; cat job-gate\",\"background\":true}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"job handed off"},"finish_reason":"stop"}]}`+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("CODEAF_BASE_URL", server.URL)
	old := environmentChild(t, workspace, os.Getenv(config.APIKeyEnv))
	first := environmentWindow(t, workspace, "")
	events, err := first.Agent().Submit(t.Context(), "start a background job")
	if err != nil {
		t.Fatal(err)
	}
	started := false
	for event := range events {
		if event.Kind == session.EventToolEnd && strings.Contains(event.Output, "job 1 started") {
			started = true
		}
	}
	if !started {
		t.Fatal("the real agent did not start its background job")
	}
	if err := first.Agent().Detach(); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Surfaces == 0 })
	t.Setenv(config.APIKeyEnv, "sk-or-v1-test-job-other-terminal")
	note, err := clearStaleEngineHost(workspace)
	if err != nil || note != differentEngineEnvironmentSentence(workspace) {
		t.Fatalf("background-job note: %q, %v", note, err)
	}
	second := environmentWindow(t, workspace, "")
	self := waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.Surfaces == 1 })
	if self.PID != old.Process.Pid {
		t.Fatal("environment change stopped a handed-off job")
	}
	// The ordinary stop door cleans up the blocked job through the real agent.
	if err := second.Agent().StopWork(); err != nil {
		t.Fatal(err)
	}
}

// C2, C12 and F5: The command a person copies from the busy note passes the
// exact workspace as one argument, including spaces and apostrophes.
func TestEnvironmentStopCommandQuotesTheWorkspaceForTheShell(t *testing.T) {
	for _, workspace := range []string{"/tmp/my project", "/tmp/it's project"} {
		t.Run(workspace, func(t *testing.T) {
			note := differentEngineEnvironmentSentence(workspace)
			_, command, ok := strings.Cut(note, ", or run ")
			if !ok || !strings.Contains(note, "and is still in use — close its other windows or let its work finish") {
				t.Fatalf("busy note does not explain the recovery: %s", note)
			}
			// A shell function records the arguments instead of stopping a host.
			out, err := exec.Command("bash", "-c", "codeaf() { printf '%s\\n' \"$@\"; }; "+command).Output()
			if err != nil || string(out) != "engine\n--stop\n--workspace\n"+workspace+"\n" {
				t.Fatalf("copied command arguments = %q, error %v", out, err)
			}
		})
	}
}
