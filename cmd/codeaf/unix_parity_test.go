package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// Unix parity 2 contract items 1 and 2: key health, fresh defaults and doctor
// are shared; desk reads must never look up or execute git/gh on Unix.
func TestUnixParitySetupHealthDoctor(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.ModelEnv, "")
	t.Setenv(config.APIKeyEnv, "sk-or-parity-fixture")
	var auth, commands atomic.Int32
	client := &http.Client{Transport: setupHealthTransport(func(*http.Request) (*http.Response, error) {
		auth.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"label":"test"}}`))}, nil
	})}
	oldClient, oldLook, oldRun := doctorKeyClient, readinessLookPath, readinessRun
	doctorKeyClient = client
	readinessLookPath = func(name string) (string, error) { commands.Add(1); return name, nil }
	readinessRun = func(context.Context, string, ...string) (string, error) {
		commands.Add(1)
		return "", errors.New("not configured")
	}
	t.Cleanup(func() { doctorKeyClient, readinessLookPath, readinessRun = oldClient, oldLook, oldRun })
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	b.keyClient = client
	b.startKeyHealth()
	if auth.Load() != 0 || commands.Load() != 0 {
		t.Fatal("launch probed readiness")
	}
	if key := b.openRouterTurnKey(config.DefaultModel); key != "sk-or-parity-fixture" {
		t.Fatalf("turn credential = %q", key)
	}
	setup, err := b.Setup()
	if err != nil || setup.DefaultModel != config.DefaultModel {
		t.Fatalf("fresh setup = %+v, %v", setup, err)
	}
	eventually(t, "shared auth check", func() bool { s, _ := b.Setup(); return s.ProviderKey.State == "ok" })
	health, err := b.HealthCheck()
	if err != nil || health.ProviderKey.State != "ok" {
		t.Fatalf("health = %+v, %v", health, err)
	}
	if auth.Load() != 1 {
		t.Fatalf("auth probes = %d; want one", auth.Load())
	}
	for _, value := range []any{setup, health} {
		raw, err := json.Marshal(value)
		if err != nil || !bytes.Contains(raw, []byte(`"provider_key"`)) {
			t.Fatalf("shared wire = %s, %v", raw, err)
		}
	}
	if runtime.GOOS != "windows" && (commands.Load() != 0 || len(health.Checks) != 0) {
		t.Fatalf("Unix desk made %d command probes: %+v", commands.Load(), health.Checks)
	}
	if err := config.WriteChatModel(config.ProfileDir(), "fixture/model"); err != nil {
		t.Fatal(err)
	}
	// A home with a saved model must keep it even before setup has been saved.
	setup, err = b.Setup()
	if err != nil || setup.DefaultModel != "fixture/model" {
		t.Fatalf("saved model proposal = %+v, %v", setup, err)
	}
	if _, err = b.SetSetup(setup); err != nil {
		t.Fatal(err)
	}
	settings, err := b.Settings()
	if err != nil || settings.DefaultModel != "fixture/model" {
		t.Fatalf("saved model = %+v, %v", settings, err)
	}
	raw, err := os.ReadFile(b.state.path)
	if err != nil || bytes.Contains(raw, []byte(`"provider_key"`)) {
		t.Fatalf("saved setup = %s, %v", raw, err)
	}
	before := commands.Load()
	var out bytes.Buffer
	if err := runDoctorWith([]string{"--db", filepath.Join(t.TempDir(), "store.db")}, &out, 0); err != nil {
		t.Fatal(err)
	}
	if commands.Load() <= before || auth.Load() != 2 {
		t.Fatalf("doctor probes: auth=%d commands=%d", auth.Load(), commands.Load())
	}
	for _, want := range []string{"provider key     ok", "git config --global user.name", "git config --global user.email", "gh auth login --web"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor missing %q: %s", want, out.String())
		}
	}
}
