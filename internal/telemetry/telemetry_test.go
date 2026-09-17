package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

// testNow is the clock every test event is built at, so hashes, ages and
// buckets are stable run to run.
var testNow = time.Date(2026, 1, 14, 9, 30, 0, 0, time.UTC)

// freshClock is the clock spool tests build their events at. The age-drop law
// is the real current time's, so an event built at any fixed stamp grows
// stale and is dropped by the contract's seven-day law — every assertion
// about what a flush sends must use a clock that agrees with the one ageOf
// reads.
func freshClock(t *testing.T) time.Time { return time.Now() }

// testHome points the state root at a directory this test chose, so nothing
// this package writes can land in the home of whoever ran `go test`, and
// clears every variable the opt-out ladder reads, so a test that wants
// telemetry on gets an environment that agrees. Every test in the package
// starts here.
func testHome(t *testing.T) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	for _, name := range []string{
		"CODEAF_TELEMETRY", "AFORGE_TELEMETRY",
		"CODEAF_TELEMETRY_ENDPOINT", "AFORGE_TELEMETRY_ENDPOINT",
		"DO_NOT_TRACK",
		"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "JENKINS_URL",
		"KUBERNETES_SERVICE_HOST",
	} {
		unsetEnv(t, name)
	}
}

// unsetEnv removes one variable for the test and puts it back afterwards, the
// way t.Setenv would if it could unset.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	old, had := os.LookupEnv(name)
	os.Unsetenv(name)
	t.Cleanup(func() {
		if had {
			os.Setenv(name, old)
			return
		}
		os.Unsetenv(name)
	})
}

func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// The ladder is driven through offReason, the one pure function every rung
// lives in. Enabled and OffReason are never asked to answer ON here: under
// `go test` they are forced off by law, so only the pure ladder can exercise
// the on rungs.

func TestLadderRunsWhenNobodyAskedItNotTo(t *testing.T) {
	testHome(t)
	if got := offReason(false, false, false); got != OnReason {
		t.Fatalf("a clean environment: ladder answered %q, want on", got)
	}
}

func TestLadderCodeafTelemetrySwitch(t *testing.T) {
	testHome(t)
	for _, value := range []string{"off", "OFF", "0", "false"} {
		t.Setenv("CODEAF_TELEMETRY", value)
		if got := offReason(false, false, false); got != OffEnv {
			t.Errorf("CODEAF_TELEMETRY=%s: got %q, want %q", value, got, OffEnv)
		}
	}
	t.Setenv("CODEAF_TELEMETRY", "on")
	if got := offReason(false, false, false); got != OnReason {
		t.Errorf("CODEAF_TELEMETRY=on: got %q, want on", got)
	}
	// The former spelling answers when the current one is unset.
	t.Setenv("CODEAF_TELEMETRY", "")
	t.Setenv("AFORGE_TELEMETRY", "off")
	if got := offReason(false, false, false); got != OffEnv {
		t.Errorf("AFORGE_TELEMETRY=off: got %q, want %q", got, OffEnv)
	}
}

func TestLadderDoNotTrackSwitch(t *testing.T) {
	testHome(t)
	for _, value := range []string{"1", "true", "TRUE", " true "} {
		t.Setenv("DO_NOT_TRACK", value)
		if got := offReason(false, false, false); got != OffDoNotTrack {
			t.Errorf("DO_NOT_TRACK=%q: got %q, want %q", value, got, OffDoNotTrack)
		}
	}
	t.Setenv("DO_NOT_TRACK", "0")
	if got := offReason(false, false, false); got != OnReason {
		t.Errorf("DO_NOT_TRACK=0: got %q, want on", got)
	}
}

func TestLadderRungsInOrder(t *testing.T) {
	testHome(t)
	if got := offReason(true, false, false); got != OffConfig {
		t.Errorf("config off: got %q, want %q", got, OffConfig)
	}
	// A person's own switch outranks the config file. Each rung is tested from
	// its own clean environment so no earlier rung is left armed.
	t.Run("env switch beats config", func(t *testing.T) {
		testHome(t)
		t.Setenv("CODEAF_TELEMETRY", "off")
		if got := offReason(true, false, false); got != OffEnv {
			t.Errorf("env off with config off: got %q, want %q", got, OffEnv)
		}
	})
	t.Run("do not track beats config", func(t *testing.T) {
		testHome(t)
		t.Setenv("DO_NOT_TRACK", "1")
		if got := offReason(true, false, false); got != OffDoNotTrack {
			t.Errorf("DO_NOT_TRACK with config off: got %q, want %q", got, OffDoNotTrack)
		}
	})
	t.Run("build honesty beats runtime", func(t *testing.T) {
		testHome(t)
		t.Setenv("DO_NOT_TRACK", "")
		if got := offReason(true, false, true); got != OffConfig {
			t.Errorf("config off with a dirty build: got %q, want %q", got, OffConfig)
		}
	})
}

func TestLadderEndpointOverride(t *testing.T) {
	testHome(t)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "")
	if got := offReason(false, false, false); got != OffEmptyEndpoint {
		t.Errorf("empty endpoint: got %q, want %q", got, OffEmptyEndpoint)
	}
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "https://example.invalid/relay")
	if got := offReason(false, false, false); got != OnReason {
		t.Errorf("set endpoint: got %q, want on", got)
	}
}

func TestLadderBuildAndTestRungs(t *testing.T) {
	testHome(t)
	if got := offReason(false, false, true); got != OffBuild {
		t.Errorf("dirty build: got %q, want %q", got, OffBuild)
	}
	if got := offReason(false, true, false); got != OffTest {
		t.Errorf("test binary: got %q, want %q", got, OffTest)
	}
	// The build's honesty outranks the runtime.
	if got := offReason(false, true, true); got != OffBuild {
		t.Errorf("dirty build in a test: got %q, want %q", got, OffBuild)
	}
}

// Enabled is asserted only in the direction `go test` forces: a test binary
// never sends, whatever the config says.
func TestEnabledIsAlwaysOffUnderGoTest(t *testing.T) {
	testHome(t)
	if Enabled(false) || Enabled(true) {
		t.Fatal("a test binary must never send telemetry")
	}
	// The build's honesty and the test binary both force off; with the config
	// off as well, the config rung answers first, per the contract's order.
	if got := OffReason(true); got != OffConfig {
		t.Fatalf("OffReason under go test with config off: got %q, want %q", got, OffConfig)
	}
	if got := OffReason(false); got != OffBuild {
		t.Fatalf("OffReason under go test from a dev checkout: got %q, want %q (an unstamped build trips the build rung)", got, OffBuild)
	}
}

func TestInstallIdentityCreatesPrivateFiles(t *testing.T) {
	testHome(t)
	hash := InstallIDHash()
	if len(hash) != 64 || !isLowerHex(hash) {
		t.Fatalf("InstallIDHash = %q, want 64 lowercase hex characters", hash)
	}
	if fi, err := os.Stat(telemetryDir()); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("telemetry dir mode = %v (%v), want 0700", fi.Mode().Perm(), err)
	}
	body, err := os.ReadFile(telemetryFile("install_id"))
	if err != nil {
		t.Fatalf("install_id must be created: %v", err)
	}
	id := strings.TrimSpace(string(body))
	if len(id) != 64 || !isLowerHex(id) {
		t.Fatalf("install_id = %q, want 64 lowercase hex characters (32 random bytes)", id)
	}
	if fi, err := os.Stat(telemetryFile("install_id")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("install_id mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
	sum := sha256.Sum256([]byte(id))
	if hex.EncodeToString(sum[:]) != hash {
		t.Error("the wire identity must be sha256 of the stored install id")
	}
	if sum := InstallIDHash(); sum != hash {
		t.Error("the same install must answer the same identity")
	}
}

func TestInstallIdentityIsStableUntilTheIDChanges(t *testing.T) {
	testHome(t)
	first := InstallIDHash()
	if again := InstallIDHash(); again != first {
		t.Fatal("the same install must answer the same identity")
	}
	if err := os.WriteFile(telemetryFile("install_id"), []byte(randomHex(32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if fresh := InstallIDHash(); fresh == first {
		t.Fatal("a replaced install id must answer a new identity")
	}
}

func TestFirstRunIsEmittedOncePerInstallID(t *testing.T) {
	testHome(t)
	if !FirstRunPending() {
		t.Fatal("a fresh install has not sent first_run yet")
	}
	MarkFirstRunSent()
	if FirstRunPending() {
		t.Fatal("after the marker, first_run must not be pending")
	}
	// A different install id is a different identity: first_run is pending again.
	if err := os.WriteFile(telemetryFile("install_id"), []byte(randomHex(32)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !FirstRunPending() {
		t.Fatal("a new install id has not sent first_run yet")
	}
}

// The notice is the contract's text, byte for byte. The contract file itself
// is not committed, so the expected text lives here.
const contractNotice = `codeaf sends anonymous usage counts to AgentField.
  Sent:  version, OS, mode (chat or task), how many sessions, how many errors.
  Never: anything about you or your work. No prompts, code, file names,
         paths, repo names, keys, email, IP, or machine name.
  See exactly what leaves:  codeaf telemetry show
  Turn off:                 CODEAF_TELEMETRY=off`

func TestNoticeIsTheContractText(t *testing.T) {
	if Notice != contractNotice {
		t.Errorf("Notice drifted from the contract:\n got: %q\nwant: %q", Notice, contractNotice)
	}
}

func TestPrintNoticeWritesOneLineOnce(t *testing.T) {
	old := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	captured := make(chan string, 1)
	go func() {
		var buf strings.Builder
		_, _ = io.Copy(&buf, reader)
		captured <- buf.String()
	}()
	PrintNotice()
	PrintNotice()
	os.Stderr = old
	writer.Close()
	got := <-captured
	if count := strings.Count(got, Notice); count != 1 {
		t.Errorf("the notice printed %d times in one process, want 1", count)
	}
}

func TestUsageContextBuckets(t *testing.T) {
	testHome(t)
	t.Setenv("CI", "1")
	if got := usageContext(); got != "ci" {
		t.Errorf("CI=1: got %q, want ci", got)
	}
	// A CI variable that says no is not CI; the next one answers.
	t.Setenv("CI", "0")
	t.Setenv("GITHUB_ACTIONS", "true")
	if got := usageContext(); got != "ci" {
		t.Errorf("GITHUB_ACTIONS=true: got %q, want ci", got)
	}
	// Outside CI a run is a container or a person's machine, never anything else.
	t.Setenv("GITHUB_ACTIONS", "")
	if got := usageContext(); got != "local" && got != "container" {
		t.Errorf("clean environment: got %q, want local or container", got)
	}
	// A container without a dockerenv file still shows as one.
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	if got := usageContext(); got != "container" {
		t.Errorf("KUBERNETES_SERVICE_HOST set: got %q, want container", got)
	}
}

func TestChannelFromRevisionShape(t *testing.T) {
	cases := []struct {
		revision string
		want     string
	}{
		{"v1.2.3", "stable"},
		{"v0.0.0", "stable"},
		{"v1.2.3-rc.1", "rc"},
		{"v10.0.2-rc.12", "rc"},
		{"dev-20260114-0123456789ab", "dev"},
		{"staging-20260114-0123456789ab", "staging"},
		{"", "unknown"},
		{"v1.2", "unknown"},
		{"v1.2.3.4", "unknown"},
		{"1.2.3", "unknown"},
		{"release-1.2.3", "unknown"},
		{"v1.2.3-rc.x", "unknown"},
		{"dev-20260114-notahexvalue", "unknown"},
		{"dev-202601-0123456789ab", "unknown"},
		{"aReleaseTag/from/somewhere", "unknown"},
	}
	for _, test := range cases {
		if got := channelFor(test.revision); got != test.want {
			t.Errorf("channelFor(%q) = %q, want %q", test.revision, got, test.want)
		}
	}
}

func TestInstallMethodReadsOnlyTheInstallerWords(t *testing.T) {
	testHome(t)
	if got := installMethod(); got != "unknown" {
		t.Errorf("no install.json: got %q, want unknown", got)
	}
	WriteInstallMethod("script")
	if got := installMethod(); got != "script" {
		t.Errorf("WriteInstallMethod(script): got %q, want script", got)
	}
	// A stray word from anywhere else is stored as unknown and read as unknown.
	WriteInstallMethod("brew install codeaf")
	if got := installMethod(); got != "unknown" {
		t.Errorf("WriteInstallMethod(brew install codeaf): got %q, want unknown", got)
	}
	if err := os.WriteFile(telemetryFile("install.json"), []byte(`{"install_method":"/Users/santosh/secret-project"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := installMethod(); got != "unknown" {
		t.Errorf("a path in install.json: got %q, want unknown", got)
	}
}

func TestEndpointOverrideAndDefault(t *testing.T) {
	testHome(t)
	if got := Endpoint(); got != DefaultEndpoint {
		t.Errorf("Endpoint() = %q, want the contract relay %q", got, DefaultEndpoint)
	}
	if DefaultEndpoint != "https://agentfield.ai/api/oss/codeaf/telemetry" {
		t.Errorf("DefaultEndpoint = %q, want the contract relay", DefaultEndpoint)
	}
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "https://example.invalid/relay")
	if got := Endpoint(); got != "https://example.invalid/relay" {
		t.Errorf("Endpoint() = %q, want the override", got)
	}
}
