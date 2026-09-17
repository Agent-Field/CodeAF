package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// telemetryHome is the clean room every telemetry-verb test runs in: a
// throwaway CODEAF_HOME and a local httptest sink pinned as the endpoint, so
// nothing a test spools can ever leave the machine (the package's own spool
// tests POST to the production relay when the endpoint is unset).
func telemetryHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "")
	t.Setenv("CODEAF_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	return root
}

// telemetrySink pins the endpoint at a local server that answers 2xx and
// swallows everything.
func telemetrySink(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", server.URL)
	return server
}

func TestTelemetryStatusReportsOn(t *testing.T) {
	telemetryHome(t)
	telemetrySink(t)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"status"}); err != nil {
		t.Fatal(err)
	}
	got := usageOut.(*strings.Builder).String()
	if !strings.HasPrefix(got, "telemetry ") {
		t.Fatalf("status should open with the state, got:\n%s", got)
	}
	if !strings.Contains(got, "endpoint") {
		t.Fatalf("status should name the endpoint, got:\n%s", got)
	}
	if !strings.Contains(got, "install") {
		t.Fatalf("status should name the install prefix, got:\n%s", got)
	}
}

func TestTelemetryStatusNamesTheReasonWhenOff(t *testing.T) {
	telemetryHome(t)
	telemetrySink(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"status"}); err != nil {
		t.Fatal(err)
	}
	got := usageOut.(*strings.Builder).String()
	if !strings.Contains(got, "telemetry off") {
		t.Fatalf("status should report off, got:\n%s", got)
	}
	if !strings.Contains(got, "CODEAF_TELEMETRY") {
		t.Fatalf("off should say why, got:\n%s", got)
	}
}

func TestTelemetryShowPrintsTheSpool(t *testing.T) {
	telemetryHome(t)
	telemetrySink(t)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"show"}); err != nil {
		t.Fatal(err)
	}
	got := usageOut.(*strings.Builder).String()
	if strings.TrimSpace(got) == "" {
		t.Fatal("show printed nothing")
	}
}

func TestTelemetryOffWritesTheSetting(t *testing.T) {
	root := telemetryHome(t)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"off"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(usageOut.(*strings.Builder).String(), "off") {
		t.Fatal("off should confirm with one line")
	}
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatalf("the profile config should exist: %v", err)
	}
	if !strings.Contains(string(data), "telemetry") {
		t.Fatalf("the config should carry the telemetry row, got:\n%s", data)
	}
}

func TestTelemetryOnUndoesOff(t *testing.T) {
	root := telemetryHome(t)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"off"}); err != nil {
		t.Fatal(err)
	}
	if err := runTelemetry([]string{"on"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatalf("the profile config should exist: %v", err)
	}
	if !strings.Contains(string(data), `"telemetry": true`) {
		t.Fatalf("on should write the row back as true, got:\n%s", data)
	}
}

func TestTelemetryBareDefaultsToStatus(t *testing.T) {
	telemetryHome(t)
	telemetrySink(t)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry(nil); err != nil {
		t.Fatal(err)
	}
	got := usageOut.(*strings.Builder).String()
	if !strings.HasPrefix(got, "telemetry ") {
		t.Fatal("a bare `telemetry` should answer with the status")
	}
}

func TestTelemetryUnknownVerbIsRefused(t *testing.T) {
	telemetryHome(t)
	if err := runTelemetry([]string{"nonsense"}); err == nil {
		t.Fatal("an unknown verb should be an error")
	}
}

func TestTelemetryIsInUsageAndEnvironmentText(t *testing.T) {
	telemetryHome(t)
	for name, text := range map[string]string{
		"usage":       usageText,
		"environment": environmentText,
	} {
		if !strings.Contains(text, "telemetry") {
			t.Errorf("%s text should name the telemetry command", name)
		}
	}
	if !strings.Contains(environmentText, "CODEAF_TELEMETRY_ENDPOINT") {
		t.Error("the environment table should name CODEAF_TELEMETRY_ENDPOINT")
	}
	if !strings.Contains(environmentText, "DO_NOT_TRACK") {
		t.Error("the environment table should name DO_NOT_TRACK")
	}
}
