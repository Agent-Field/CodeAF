package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
)

// telemetryHome is the clean room every telemetry-verb test runs in: a
// throwaway CODEAF_HOME and a local httptest sink pinned as the endpoint, so
// nothing a test spools can ever leave the machine (the package's own spool
// tests POST to the production relay when the endpoint is unset).
//
// AND THE PROFILE IS PINNED WITH THE STATE ROOT, because `telemetry on` and
// `telemetry off` write the profile's own row through config.WriteTelemetry with
// the directory config.ProfileDir() resolves. An exported CODEAF_PROFILE_DIR —
// which the harness that runs this suite sets at a live profile — outranks
// CODEAF_HOME, so a test that moved only the state root wrote that row, and the
// spool it read back, into somebody else's profile.
func telemetryHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv(config.ProfileDirEnv, "")
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

// seedPoolOutbox writes one judged row into the profile's pool outbox the way
// a run would — through the outbox's own open and append, closed again — so
// the show verb reads what stands on disk.
func seedPoolOutbox(t *testing.T, root, payload string) {
	t.Helper()
	box, err := outbox.Open(filepath.Join(root, "pool", "outbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Append([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := box.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestTelemetryShowPrintsBothStreams is the law behind the notice's "see
// exactly what leaves": the Model Pool's rows go to a different relay under a
// different switch, and a show that printed only the usage-count spool let a
// person believe CODEAF_TELEMETRY=off stopped everything. Both streams print,
// each under a line naming where it goes, and the pool row's bytes are the
// bytes the relay would receive.
func TestTelemetryShowPrintsBothStreams(t *testing.T) {
	root := telemetryHome(t)
	telemetrySink(t)
	t.Setenv("CODEAF_MODEL_POOL", "on")
	seedPoolOutbox(t, root, `{"schema":1,"metric":"role_quality","role":"worker","model":"vendor/model-x","score":81}`)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"show"}); err != nil {
		t.Fatal(err)
	}
	got := usageOut.(*strings.Builder).String()
	for _, want := range []string{
		"usage counts (",
		// The suite's TestMain pins the relay at an unreachable local address
		// so no test can post to the real one; the heading names whatever
		// submit address is in force, and the path is the relay's own.
		"Model Pool (http",
		"/v1/rows)",
		`"model":"vendor/model-x"`,
		`"score":81`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("show should print %q, got:\n%s", want, got)
		}
	}
	if strings.Count(got, "[") < 2 {
		t.Errorf("show should print one JSON array per stream, got:\n%s", got)
	}
}

// TestTelemetryShowSaysWhenThePoolSendsNothing pins the heading for a pool in
// `read`: the rows that wait are still printed, under a line saying nothing is
// sent, so the person is not told a destination that nothing goes to.
func TestTelemetryShowSaysWhenThePoolSendsNothing(t *testing.T) {
	root := telemetryHome(t)
	telemetrySink(t)
	t.Setenv("CODEAF_MODEL_POOL", "read")
	seedPoolOutbox(t, root, `{"n":1}`)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"show"}); err != nil {
		t.Fatal(err)
	}
	got := usageOut.(*strings.Builder).String()
	if !strings.Contains(got, "Model Pool (model_pool read, nothing is sent)") {
		t.Errorf("a pool in read should say nothing is sent, got:\n%s", got)
	}
	if !strings.Contains(got, `{"n":1}`) {
		t.Errorf("the waiting row should still print, got:\n%s", got)
	}
}

// TestTelemetryShowDoesNotCreateThePoolOutbox is the reading-form law: a show
// on a profile with no outbox prints `[]` for the pool and leaves no file
// behind, because [outbox.Open] creates an absent outbox and a reader must not.
func TestTelemetryShowDoesNotCreateThePoolOutbox(t *testing.T) {
	root := telemetryHome(t)
	telemetrySink(t)
	usageOut = &strings.Builder{}
	defer func() { usageOut = os.Stdout }()
	if err := runTelemetry([]string{"show"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "pool", "outbox.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("show must not create the pool outbox, stat: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(usageOut.(*strings.Builder).String()), "[]") {
		t.Fatalf("an empty pool should print [], got:\n%s", usageOut.(*strings.Builder).String())
	}
}
