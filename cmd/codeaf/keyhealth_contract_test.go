package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/shell"
)

type setupHealthTransport func(*http.Request) (*http.Response, error)

func (f setupHealthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Unix parity 2 contract items 1/2 (shared key health and doctor).
// keyhealth-fix contract 1, 2 and 4: launch stays quiet; the first setup or
// health read starts one check without waiting, and both share its result.
func TestSetupContractLaunchKeyHealth(t *testing.T) {
	for _, test := range []struct{ firstRead, state string }{
		{"setup", "ok"}, {"setup", "invalid"}, {"setup", "unknown"},
		{"health", "ok"}, {"health", "invalid"}, {"health", "unknown"},
	} {
		t.Run(test.firstRead+"/"+test.state, func(t *testing.T) {
			state := test.state
			t.Setenv(home.EnvVar, t.TempDir())
			t.Setenv(config.ProfileDirEnv, t.TempDir())
			t.Setenv(config.APIKeyEnv, "sk-or-test-launch")
			t.Setenv("CODEAF_BASH", filepath.Join(t.TempDir(), "missing-bash"))
			oldLook := readinessLookPath
			readinessLookPath = func(string) (string, error) { return "", errors.New("not installed") }
			t.Cleanup(func() { readinessLookPath = oldLook })
			var calls atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					close(entered)
				}
				if r.URL.Path != "/key" || r.Header.Get("Authorization") != "Bearer sk-or-test-launch" {
					t.Errorf("unexpected auth request: %s", r.URL.Path)
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if state == "invalid" {
					w.WriteHeader(401)
					return
				}
				if state == "unknown" {
					w.WriteHeader(503)
					return
				}
				_, _ = w.Write([]byte(`{"data":{"label":"test"}}`))
			}))
			defer server.Close()
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			b := newFakeBackend(newAppRecorder(), nil)
			defer b.Close()
			b.keyAddress = func(string) string { return server.URL }
			b.startKeyHealth()
			select {
			case <-entered:
				t.Fatal("launch checked auth before a setup or health read")
			case <-time.After(100 * time.Millisecond):
			}
			var key appwire.ProviderKeyHealth
			if test.firstRead == "setup" {
				setup, err := b.Setup()
				if err != nil {
					t.Fatal(err)
				}
				key = setup.ProviderKey
			} else {
				health, err := b.HealthCheck()
				if err != nil {
					t.Fatal(err)
				}
				key = health.ProviderKey
			}
			// Returning before this blocked probe has a timestamp proves the
			// read did not wait for it, without a scheduler-speed assertion.
			if key.State != "unknown" || key.CheckedAt != nil {
				t.Fatalf("in-flight %s waited for auth: %+v", test.firstRead, key)
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("first read did not start the auth check")
			}
			close(release)
			eventually(t, "key health checked", func() bool { s, _ := b.Setup(); return s.ProviderKey.CheckedAt != nil })
			var setup appwire.Setup
			var err error
			for range 2 {
				setup, err = b.Setup()
				if err != nil || setup.ProviderKey.State != state || setup.ProviderKey.CheckedAt == nil {
					t.Fatalf("key health = %+v, %v", setup.ProviderKey, err)
				}
				if state == "invalid" && setup.ProviderKey.Message != "Your OpenRouter key stopped working. Sign in again." {
					t.Fatalf("invalid message = %q", setup.ProviderKey.Message)
				}
			}
			health, err := b.HealthCheck()
			if err != nil || health.ProviderKey.State != state || !health.ProviderKey.CheckedAt.Equal(*setup.ProviderKey.CheckedAt) {
				t.Fatalf("health key = %+v, %v", health.ProviderKey, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("%d auth calls; want one check after the first read", calls.Load())
			}
		})
	}
}

// Unix parity 2 contract items 1/2 (shared key health and doctor).
// keyhealth-fix contract 2: a hung auth endpoint becomes unknown with a checked timestamp.
func TestSetupContractKeyHealthTimeout(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "sk-or-test-timeout")
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	b.keyClient = &http.Client{Transport: setupHealthTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	b.startKeyHealth()
	eventually(t, "auth timeout", func() bool { s, _ := b.Setup(); return s.ProviderKey.CheckedAt != nil })
	s, _ := b.Setup()
	if s.ProviderKey.State != "unknown" {
		t.Fatalf("timeout = %+v", s.ProviderKey)
	}
}

// Unix parity 2 contract items 1/2 (shared key health and doctor).
// keyhealth-fix contract 3: doctor still probes auth directly without a model
// call. Setup contract 4 reports command failures with actionable hints while
// retaining the normal GH_TOKEN.
func TestSetupContractDoctorReadiness(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "sk-or-test-doctor")
	t.Setenv("GH_TOKEN", "test-normal-environment")
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/key" {
			t.Errorf("doctor requested %s", r.URL.Path)
		}
		if healthy.Load() {
			_, _ = w.Write([]byte(`{"data":{"label":"test"}}`))
			return
		}
		w.WriteHeader(401)
	}))
	defer server.Close()
	t.Setenv("CODEAF_BASE_URL", server.URL)
	oldClient, oldRun, oldLook := doctorKeyClient, readinessRun, readinessLookPath
	doctorKeyClient = server.Client()
	readinessLookPath = func(name string) (string, error) { return name, nil }
	readinessRun = func(ctx context.Context, name string, args ...string) (string, error) {
		if _, bounded := ctx.Deadline(); !bounded {
			t.Error("command has no timeout")
		}
		if healthy.Load() {
			return "configured", nil
		}
		switch strings.Join(append([]string{name}, args...), " ") {
		case "git config user.name":
			return "", errors.New("unset")
		case "git config user.email":
			return "", errors.New("unset")
		case "gh auth status":
			if got := os.Getenv("GH_TOKEN"); got != "test-normal-environment" {
				t.Errorf("normal GH_TOKEN lost: %q", got)
			}
			return "", errors.New("not signed in")
		default:
			t.Errorf("unexpected probe %s %v", name, args)
			return "", errors.New("unexpected")
		}
	}
	t.Cleanup(func() { doctorKeyClient, readinessRun, readinessLookPath = oldClient, oldRun, oldLook })
	var out bytes.Buffer
	if err := runDoctorWith([]string{"--db", filepath.Join(t.TempDir(), "store.db")}, &out, 0); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"invalid", "Your OpenRouter key stopped working. Sign in again.", "git config --global user.name", "git config --global user.email", "gh auth login --web"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor missing %q:\n%s", want, out.String())
		}
	}
	healthBackend := newFakeBackend(newAppRecorder(), nil)
	defer healthBackend.Close()
	health, err := healthBackend.HealthCheck()
	if err != nil || (runtime.GOOS == "windows" && len(health.Checks) < 3) || (runtime.GOOS != "windows" && len(health.Checks) != 0) {
		t.Fatalf("app health readiness = %+v, %v", health, err)
	}
	healthy.Store(true)
	out.Reset()
	if err := runDoctorWith([]string{"--db", filepath.Join(t.TempDir(), "store.db")}, &out, 0); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"provider key     ok", "gh is signed in", "git found"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("healthy doctor missing %q:\n%s", want, out.String())
		}
	}
	readinessLookPath = func(name string) (string, error) { return "", errors.New("missing") }
	out.Reset()
	if err := runDoctorWith([]string{"--db", filepath.Join(t.TempDir(), "store.db")}, &out, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "info · gh is not installed") || !strings.Contains(out.String(), "install Git") {
		t.Fatalf("missing tools:\n%s", out.String())
	}
}

// Unix parity 2 contract items 1/2 (shared key health and doctor).
// keyhealth-fix contract 2: a real turn's auth refusal invalidates the cached reading
// without a second probe.
func TestSetupContractTurnAuthRefusal(t *testing.T) {
	for _, readBeforeTurn := range []bool{false, true} {
		name := "before-first-read"
		if readBeforeTurn {
			name = "after-first-read"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			t.Setenv(config.ProfileDirEnv, t.TempDir())
			t.Setenv(config.APIKeyEnv, "sk-or-test-turn")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"data":{"label":"test"}}`))
			}))
			defer server.Close()
			conv := &fakeConversation{script: []session.Event{{Kind: session.EventError, ErrKind: session.TurnErrorProvider, ErrStatus: 401, ErrWords: "not authorized"}}}
			var mu sync.Mutex
			var opens, releases int
			rec := newAppRecorder()
			b := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
			defer b.Close()
			b.keyAddress = func(string) string { return server.URL }
			b.startKeyHealth()
			if readBeforeTurn {
				eventually(t, "valid key after first read", func() bool { s, _ := b.Setup(); return s.ProviderKey.State == "ok" })
			}
			agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir(), Model: config.DefaultModel})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Send(agent.ID, "hello", ""); err != nil {
				t.Fatal(err)
			}
			rec.waitFor(t, 5*time.Second, appTurnDone)
			setup, _ := b.Setup()
			if setup.ProviderKey.State != "invalid" {
				t.Fatalf("401 left setup health %+v", setup.ProviderKey)
			}
			wantCalls := int32(0)
			if readBeforeTurn {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatalf("401 caused %d auth calls; want %d", calls.Load(), wantCalls)
			}
		})
	}
}

// Unix parity 2 contract items 1/2 (shared key health and doctor).
// keyhealth-fix contract 2: a handover starts the check even before a read;
// unchanged keys dedupe, while a new key or address gets a fresh check.
func TestSetupContractKeyHealthHandover(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	var calls atomic.Int32
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		requests <- r.URL.Path + " " + r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{"label":"test"}}`))
	}))
	defer server.Close()
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	address := server.URL + "/first"
	b.keyAddress = func(string) string { return address }
	b.startKeyHealth()
	if err := b.SetKey("openrouter", "sk-or-test-first"); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requests:
		if request != "/first/key Bearer sk-or-test-first" {
			t.Fatalf("handover request = %q", request)
		}
	case <-time.After(time.Second):
		t.Fatal("key handover did not start a check before a read")
	}
	eventually(t, "handed key checked", func() bool { s, _ := b.Setup(); return s.ProviderKey.State == "ok" })
	if err := b.SetKey("openrouter", "sk-or-test-first"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Setup(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("same key made %d auth checks; want one", calls.Load())
	}
	if err := b.SetKey("openrouter", "sk-or-test-second"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "replacement key checked", func() bool { s, _ := b.Setup(); return s.ProviderKey.State == "ok" })
	address = server.URL + "/second"
	if _, err := b.Setup(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "replacement address checked", func() bool { s, _ := b.Setup(); return s.ProviderKey.State == "ok" })
	if calls.Load() != 3 {
		t.Fatalf("key and address replacements made %d auth checks; want three", calls.Load())
	}
	for _, want := range []string{"/first/key Bearer sk-or-test-second", "/second/key Bearer sk-or-test-second"} {
		select {
		case request := <-requests:
			if request != want {
				t.Fatalf("replacement request = %q; want %q", request, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing replacement check %q", want)
		}
	}
}

// Unix parity 2 contract items 1/2 (shared key health and doctor).
// keyhealth-fix contract 1, 2 and 4 through a real app process and wire reads.
// A daemon owns its process globals, so each launch gets a fresh process too.
func TestSetupContractAppLaunchWire(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	releaseProbe := sync.OnceFunc(func() { close(release) })
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Catalog warming is independent of the key-auth check under test.
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		if r.URL.Path != "/key" {
			t.Errorf("unexpected auth request: %s", r.URL.Path)
		}
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer auth.Close()
	defer releaseProbe()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, self, "-test.run=^TestSetupContractAppLaunchProcess$")
	command.Env = append(os.Environ(), "CODEAF_TEST_SETUP_APP_CHILD=1", "CODEAF_TEST_SETUP_URL="+auth.URL, "CODEAF_TEST_SETUP_STATE="+t.TempDir(), "CODEAF_TEST_SETUP_PROFILE="+t.TempDir())
	var stderr bytes.Buffer
	command.Stderr = &stderr
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	defer input.Close()
	lines := bufio.NewScanner(output)
	if !lines.Scan() {
		t.Fatalf("app did not say hello: %v", lines.Err())
	}
	var hello map[string]any
	if err := json.Unmarshal(lines.Bytes(), &hello); err != nil || hello["event"] != "hello" {
		t.Fatalf("app hello = %s, %v", lines.Bytes(), err)
	}
	select {
	case <-entered:
		t.Fatal("app launch checked auth before a setup or health read")
	case <-time.After(100 * time.Millisecond):
	}
	readKey := func(method, field string) map[string]any {
		t.Helper()
		if _, err := io.WriteString(input, `{"id":1,"method":"`+method+`"}`+"\n"); err != nil {
			t.Fatal(err)
		}
		for lines.Scan() {
			var line map[string]any
			if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
				t.Fatal(err)
			}
			if line["id"] != float64(1) {
				continue
			}
			if line["error"] != nil {
				t.Fatalf("%s: %v", method, line)
			}
			result := line["result"].(map[string]any)[field].(map[string]any)
			return result["provider_key"].(map[string]any)
		}
		t.Fatalf("app output ended: %v", lines.Err())
		return nil
	}
	key := readKey("setup.get", "setup")
	if key["state"] != "unknown" || key["checked_at"] != nil {
		t.Fatalf("first setup read waited for auth: %v", key)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first setup read did not start the auth check")
	}
	key = readKey("health.check", "health")
	if key["state"] != "unknown" || key["checked_at"] != nil {
		t.Fatalf("in-flight health read waited for auth: %v", key)
	}
	releaseProbe()
	eventually(t, "auth after setup.get", func() bool {
		key = readKey("setup.get", "setup")
		return key["state"] == "invalid" && key["message"] == invalidOpenRouterKey && key["checked_at"] != nil
	})
	for range 2 {
		health := readKey("health.check", "health")
		if health["state"] != "invalid" || health["checked_at"] != key["checked_at"] {
			t.Fatalf("health did not share setup's reading: %v, %v", health, key)
		}
	}
	input.Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("app launch: %v: %s", err, stderr.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("app made %d auth checks; want one after the first read", calls.Load())
	}
}

func TestSetupContractAppLaunchProcess(t *testing.T) {
	if os.Getenv("CODEAF_TEST_SETUP_APP_CHILD") != "1" {
		return
	}
	state := os.Getenv("CODEAF_TEST_SETUP_STATE")
	t.Setenv(home.EnvVar, state)
	t.Setenv(config.ProfileDirEnv, os.Getenv("CODEAF_TEST_SETUP_PROFILE"))
	t.Setenv(config.APIKeyEnv, "sk-or-app-launch-fixture")
	t.Setenv("CODEAF_BASE_URL", os.Getenv("CODEAF_TEST_SETUP_URL"))
	// Resolve Windows shell readiness before the blocked auth probe starts.
	_, _ = shell.Bash()
	readinessLookPath = func(string) (string, error) { return "", errors.New("not installed") }
	flushFinalSaves = func(context.Context) error { return nil }
	if err := runApp([]string{"--home", state}); err != nil {
		t.Fatal(err)
	}
}
