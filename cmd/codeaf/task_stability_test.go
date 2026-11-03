package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/session"
)

// S8 proves the ordinary do entry retains the original store and worker
// defaults even beside a file that would tighten a task door child's limits.
func TestDoWithoutTheHiddenFlagKeepsItsStoreAndControls(t *testing.T) {
	profile := beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	work := beltRepoWorkspace(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(work), "request.json"), []byte(`{"max_steps":1,"no_progress":1,"effort":"high","limits":{"usd":0.00001}}`), 0600); err != nil {
		t.Fatal(err)
	}
	base := finishingSeat(0.01)
	seat := &beltSeat{ever: func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		if got := provider.ReasoningEffortFrom(ctx); got == provider.EffortHigh {
			t.Error("ordinary do inherited remote effort")
		}
		return base.CompleteWithMessages(ctx, messages)
	}}
	out, err := runErrand(doRequest{task: "write out.txt", workspace: work, keep: true, timeout: 45 * time.Second,
		stderr: &strings.Builder{}, newBeltCompleter: func(string) session.Completer { return seat }}, stubErrandSeats())
	if err != nil || out.status() != 0 {
		t.Fatalf("ordinary do was constrained by task controls: %+v, %v", out, err)
	}
	if !strings.HasPrefix(out.recordKept, filepath.Join(profile, "runs", "codeaf-do-")) {
		t.Fatalf("ordinary do left headlessStore: %s", out.recordKept)
	}
	paths, _ := filepath.Glob(filepath.Join(out.recordKept, "tasks", "*", "trajectory.jsonl"))
	if len(paths) == 0 {
		t.Fatal("the normal kept run has no trajectory")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, encoded := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var line map[string]json.RawMessage
			if err := json.Unmarshal([]byte(encoded), &line); err != nil {
				t.Fatal(err)
			}
			if line["recorded_order"] != nil || line["recorded_at"] != nil {
				t.Fatalf("ordinary do wrote task-only ordering: %s", encoded)
			}
		}
	}
}

// S1 includes building v3Shape for every conversation, including linked windows.
func TestBuildingAPairedConversationDialsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("paired tasks require Linux or macOS")
	}
	profile := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()
	t.Setenv("CODEAF_SYNC_URL", server.URL)
	if _, err := identity.Ensure(profile); err != nil {
		t.Fatal(err)
	}
	for _, lanes := range []v3Lanes{v3LanesHere(), v3LanesOverWire()} {
		cfg, _ := v3Shape(session.Config{}, lanes)
		if cfg.TaskComputers == nil {
			t.Fatal("paired home lost the person's placement door")
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("building the conversation made %d relay calls", got)
	}
}

// S1 requires construction to read local identity material without repairing
// or creating it. A missing device key cannot opt a home into task placement.
func TestBuildingATaskDoorNeverCreatesAMissingDevice(t *testing.T) {
	profile := pairedTaskDefaultHome(t)
	// The identity API owns the paths; removing its device file models an
	// interrupted setup without creating a second identity.
	devicePath := filepath.Join(profile, identity.DeviceFile)
	if err := os.Remove(devicePath); err != nil {
		t.Fatal(err)
	}
	if computers := v3TaskComputers(profile); computers != nil {
		t.Fatal("construction repaired a missing device")
	}
	if _, err := os.Stat(devicePath); !os.IsNotExist(err) {
		t.Fatalf("construction created a device file: %v", err)
	}
}

// S5 exercises the real command while preserving the test process's signals.
func TestPlainServeHelper(t *testing.T) {
	if os.Getenv("CODEAF_TEST_PLAIN_SERVE") != "1" {
		return
	}
	os.Setenv("HOME", os.Getenv("CODEAF_TEST_SERVE_HOME"))
	os.Setenv("CODEAF_HOME", os.Getenv("CODEAF_TEST_SERVE_HOME"))
	os.Setenv("CODEAF_PROFILE_DIR", "")
	os.Setenv("CODEAF_SYNC_URL", os.Getenv("CODEAF_TEST_SERVE_RELAY"))
	err := runServe([]string{"--via", os.Getenv("CODEAF_TEST_SERVE_RELAY")})
	if err != nil {
		t.Fatal(err)
	}
}

// S5 keeps plain serve's sole registration and output on the machine door.
func TestPlainServeOpensNoTaskDoor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture stops serve with a Unix signal")
	}
	profile := t.TempDir()
	if _, err := identity.Ensure(profile); err != nil {
		t.Fatal(err)
	}
	var registrations atomic.Int32
	machine := &relay.Server{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/engine" {
			registrations.Add(1)
		}
		machine.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer server.CloseClientConnections()
	log := filepath.Join(profile, "serve.log")
	output, err := os.Create(log)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlainServeHelper$")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_PLAIN_SERVE=1", "CODEAF_TEST_SERVE_HOME="+profile, "CODEAF_TEST_SERVE_RELAY="+server.URL, "CODEAF_HOME="+filepath.Join(profile, "bootstrap"), "HOME="+filepath.Join(profile, "bootstrap"), "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1")
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, _ := os.ReadFile(log)
		if strings.Contains(string(raw), "this machine is reachable as") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve did not register: %s", raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		raw, _ := os.ReadFile(log)
		t.Fatalf("serve: %v\n%s", err, raw)
	}
	raw, _ := os.ReadFile(log)
	if registrations.Load() != 1 || strings.Contains(string(raw), "tasks") {
		t.Fatalf("plain serve added a task door: registrations=%d\n%s", registrations.Load(), raw)
	}
	if _, err := os.Stat(filepath.Join(profile, "v3", "taskdoor")); !os.IsNotExist(err) {
		t.Fatalf("plain serve touched task storage: %v", err)
	}
}

// S5 makes receiving tasks an explicit, discoverable command choice.
func TestServeHelpOffersTakeTasks(t *testing.T) {
	out, _ := captureUsage(t)
	if code := exitCodeOf(runServe([]string{"--help"})); code != 0 {
		t.Fatalf("serve help exit=%d", code)
	}
	if !strings.Contains(out.String(), "--take-tasks") {
		t.Fatalf("serve help hides the opt-in: %s", out.String())
	}
}
