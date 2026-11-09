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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

type setupHealthTransport func(*http.Request) (*http.Response, error)

func (f setupHealthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Contract 3: launch does not wait for auth, and setup and health expose the
// same timestamped result without another request, including unknown offline.
func TestSetupContractLaunchKeyHealth(t *testing.T) {
	for _, state := range []string{"ok", "invalid", "unknown"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv(home.EnvVar, t.TempDir())
			t.Setenv(config.ProfileDirEnv, t.TempDir())
			t.Setenv(config.APIKeyEnv, "sk-or-test-launch")
			var calls atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/key" || r.Header.Get("Authorization") != "Bearer sk-or-test-launch" {
					t.Errorf("unexpected auth request: %s", r.URL.Path)
				}
				close(entered)
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
			case <-time.After(time.Second):
				t.Fatal("launch did not check auth")
			}
			setup, err := b.Setup()
			if err != nil || setup.ProviderKey.State != "unknown" {
				t.Fatalf("in-flight setup = %+v, %v", setup, err)
			}
			close(release)
			eventually(t, "key health checked", func() bool { s, _ := b.Setup(); return s.ProviderKey.CheckedAt != nil })
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
				t.Fatalf("%d auth calls; want one launch check", calls.Load())
			}
		})
	}
}

// Contract 3: a hung auth endpoint becomes unknown with a checked timestamp.
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

// Contract 3 and 4: doctor probes auth without a model call, and reports
// command failures with actionable hints while retaining the normal GH_TOKEN.
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
	if err != nil || len(health.Checks) < 3 {
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

// Contract 3: a real turn's auth refusal invalidates the launch reading
// without a second probe.
func TestSetupContractTurnAuthRefusal(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "sk-or-test-turn")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":{"label":"test"}}`)) }))
	defer server.Close()
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventError, ErrKind: session.TurnErrorProvider, ErrStatus: 401, ErrWords: "not authorized"}}}
	var mu sync.Mutex
	var opens, releases int
	rec := newAppRecorder()
	b := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
	defer b.Close()
	b.keyAddress = func(string) string { return server.URL }
	b.startKeyHealth()
	eventually(t, "valid launch key", func() bool { s, _ := b.Setup(); return s.ProviderKey.State == "ok" })
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
}

// Contract 3 through a real app process and setup.get. A daemon owns its
// process globals for its lifetime, so each launch gets a fresh process too.
func TestSetupContractAppLaunchWire(t *testing.T) {
	var calls atomic.Int32
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			calls.Add(1)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer auth.Close()
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
	eventually(t, "launch auth on setup.get", func() bool {
		if _, err := io.WriteString(input, `{"id":1,"method":"setup.get"}`+"\n"); err != nil {
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
				t.Fatalf("setup.get: %v", line)
			}
			setup := line["result"].(map[string]any)["setup"].(map[string]any)
			key := setup["provider_key"].(map[string]any)
			return key["state"] == "invalid" && key["message"] == invalidOpenRouterKey && key["checked_at"] != nil
		}
		t.Fatalf("app output ended: %v", lines.Err())
		return false
	})
	input.Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("app launch: %v: %s", err, stderr.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("launch made %d auth checks", calls.Load())
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
	flushFinalSaves = func(context.Context) error { return nil }
	if err := runApp([]string{"--home", state}); err != nil {
		t.Fatal(err)
	}
}
