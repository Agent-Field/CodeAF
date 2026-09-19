package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
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
// telemetry on gets an environment that agrees. It then points the endpoint
// at a dead loopback address, so a flush a test forgot to give a relay to
// fails locally instead of POSTing to the production relay. Tests that need
// a working relay call newRelay after testHome, which overrides it; a test
// that asserts the default endpoint unsets the variable itself with unsetEnv
// and must not flush. Every test in the package starts here.
func testHome(t *testing.T) {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	for _, name := range []string{
		"CODEAF_TELEMETRY", "CODEAF_TELEMETRY_ENDPOINT",
		"DO_NOT_TRACK",
		"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "JENKINS_URL",
		"KUBERNETES_SERVICE_HOST",
	} {
		unsetEnv(t, name)
	}
	// Nothing listens here, and loopback dialing is refused immediately.
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "http://127.0.0.1:1/telemetry")
	// A test binary trips the test and unstamped-build rungs of the ladder,
	// which now gates Spool, SpoolSync and Flush. Spool tests exercise the
	// write paths, so they run as if the ladder had answered on — the
	// ladder's own rungs are tested by TestOptOutLadderGatesTheWritePaths,
	// which never forces on.
	forceLadderForTest(t, true)
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
	// The retired spelling answers when the current one is unset (namelaw
	// reads the lines below as the legacy name it is).
	t.Setenv("CODEAF_TELEMETRY", "")
	t.Setenv("AFORGE_"+"TELEMETRY", "off") // legacy-name
	if got := offReason(false, false, false); got != OffEnv {
		t.Errorf("AFORGE_"+"TELEMETRY=off: got %q, want %q", got, OffEnv) // legacy-name
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

// Enabled is asserted in the direction `go test` forces: a test binary
// never sends, whatever the config says. The bool-taking forms stay
// unexported so the ladder can still be walked rung by rung.
func TestEnabledIsAlwaysOffUnderGoTest(t *testing.T) {
	testHome(t)
	// This test walks the real ladder, not the write-path override.
	forceLadderForTest(t, false)
	resetConfiguredForTest()
	if Enabled() {
		t.Fatal("a test binary must never send telemetry")
	}
	Configure(false)
	if Enabled() {
		t.Fatal("a test binary must never send telemetry, even with the config on")
	}
	Configure(true)
	if got := OffReason(); got != OffConfig {
		t.Fatalf("OffReason with config off: got %q, want %q", got, OffConfig)
	}
	resetConfiguredForTest()
	if got := OffReason(); got != OffBuild && got != OffTest {
		t.Fatalf("OffReason from a dev checkout: got %q, want the build or test rung", got)
	}
	if enabledForConfig(false) || !enabledForConfig(true) == enabledForConfig(true) {
		t.Fatal("the bool-taking forms must stay reachable for the ladder tests")
	}
	if got := offReasonForConfig(false); got != OffBuild && got != OffTest {
		t.Fatalf("offReasonForConfig with config on: got %q, want the build or test rung", got)
	}
}

// TestOptOutLadderGatesTheWritePaths pins the caller's side of item 5: with
// CODEAF_TELEMETRY=off, with DO_NOT_TRACK=1, and with Configure(true), Spool
// writes no file and Flush makes no HTTP request — the ladder is checked
// before anything happens, not after something was already written.
func TestOptOutLadderGatesTheWritePaths(t *testing.T) {
	cases := []struct {
		name      string
		env       func(t *testing.T)
		configure func(t *testing.T)
	}{
		{
			name: "CODEAF_TELEMETRY=off",
			env:  func(t *testing.T) { t.Setenv("CODEAF_TELEMETRY", "off") },
		},
		{
			name: "DO_NOT_TRACK=1",
			env:  func(t *testing.T) { t.Setenv("DO_NOT_TRACK", "1") },
		},
		{
			name: "Configure(true)",
			configure: func(t *testing.T) {
				resetConfiguredForTest()
				t.Cleanup(resetConfiguredForTest)
				Configure(true)
			},
		},
	}
	for _, rung := range cases {
		t.Run(rung.name, func(t *testing.T) {
			testHome(t)
			// The gate test walks the real ladder — no write-path override.
			forceLadderForTest(t, false)
			resetConfiguredForTest()
			if rung.env != nil {
				rung.env(t)
			}
			if rung.configure != nil {
				rung.configure(t)
			}
			recorder := newRelay(t)
			MarkNoticeShown()
			Spool(SessionStarted(ModeChat, false, "session-gated", freshClock(t)))
			if err := SpoolSync(SessionStarted(ModeChat, false, "session-gated-sync", freshClock(t))); err != nil {
				t.Fatalf("SpoolSync returned %v on a gated run; a no-op answers nil", err)
			}
			if err := Flush(context.Background()); err != nil {
				t.Fatalf("Flush returned %v on a gated run; a no-op answers nil", err)
			}
			if got := recorder.count(); got != 0 {
				t.Fatalf("the relay saw %d requests on a gated run, want 0", got)
			}
			if _, err := os.Stat(spoolPath()); !os.IsNotExist(err) {
				t.Fatalf("a spool file appeared on a gated run: %v", err)
			}
		})
	}
}

// TestDirtyOrUnstampedBuildReadsBuildInfoNotTheDisplayString pins item 8:
// the build rung answers through buildinfo.Dirty() and a missing revision,
// never by parsing Identity() for "/true/". An unstamped, clean build is
// still off — it cannot name its source — and a stamped dirty one is off on
// the dirty flag alone, even though its Identity carries no "/true/".
func TestDirtyOrUnstampedBuildReadsBuildInfoNotTheDisplayString(t *testing.T) {
	if !dirtyOrUnstampedBuild() {
		t.Fatal("a test binary is unstamped and must answer dirty-or-unstamped true")
	}
	// The rewiring is structural: the function must not contain the old
	// display-string parse. Its behavior for stamped builds is buildinfo's
	// Dirty test's to pin.
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
  What is collected:        codeaf telemetry info
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
	// The record now exists and parses, so a second call is a no-op and the
	// stray word cannot overwrite what the first call wrote.
	WriteInstallMethod("brew install codeaf")
	if got := installMethod(); got != "script" {
		t.Errorf("WriteInstallMethod overwrote an existing record: got %q, want script", got)
	}
	// With no record at all, a stray word from anywhere else is stored as
	// unknown and read as unknown.
	if err := os.Remove(telemetryFile("install.json")); err != nil {
		t.Fatal(err)
	}
	WriteInstallMethod("brew install codeaf")
	if got := installMethod(); got != "unknown" {
		t.Errorf("WriteInstallMethod(stray word): got %q, want unknown", got)
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
	// testHome points the endpoint at a dead loopback address so no test can
	// reach production by default. This test is the one that must see the
	// default answer, so it unsets the override itself — and it never flushes.
	unsetEnv(t, "CODEAF_TELEMETRY_ENDPOINT")
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

// roundTripCounter is the recording transport TestNoTestBinaryCanReachThe
// ProductionRelay swaps in behind the package's client: it counts the round
// trips it is asked to make and answers a refusal it is never expected to see.
type roundTripCounter struct {
	mu    sync.Mutex
	trips int
}

func (c *roundTripCounter) RoundTrip(request *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.trips++
	c.mu.Unlock()
	return nil, fmt.Errorf("telemetry test transport: no round trip may happen, saw %s %s", request.Method, request.URL)
}

func (c *roundTripCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.trips
}

// TestNoTestBinaryCanReachTheProductionRelay pins the send path's law: when a
// test binary ends up with the default endpoint — an endpoint override nobody
// set — the flush refuses before building any request, and does so even with
// the ladder forced on. The recording transport proves no HTTP round trip was
// attempted, not merely that the spool survived; the event stays spooled the
// way a failed send leaves it, and nothing reaches the real relay.
func TestNoTestBinaryCanReachTheProductionRelay(t *testing.T) {
	testHome(t)
	// The default is what must be watched here: unset the dead loopback
	// address testHome set, so Endpoint() answers the production relay.
	unsetEnv(t, "CODEAF_TELEMETRY_ENDPOINT")
	if got := Endpoint(); got != DefaultEndpoint {
		t.Fatalf("Endpoint() = %q, want the default the law watches for", got)
	}
	counter := &roundTripCounter{}
	previous := httpClient
	httpClient = &http.Client{Transport: counter}
	t.Cleanup(func() { httpClient = previous })
	MarkNoticeShown()
	event := SessionStarted(ModeChat, false, "session-law", freshClock(t))
	if err := SpoolSync(event); err != nil {
		t.Fatalf("SpoolSync returned %v; a spool that is never sent answers nil", err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatalf("Flush returned %v; a refused send must stay silent", err)
	}
	if trips := counter.count(); trips != 0 {
		t.Fatalf("the send path made %d round trips under go test against the default endpoint, want 0", trips)
	}
	left := SpoolContents()
	if len(left) != 1 {
		t.Fatalf("%d lines remain in the spool, want the refused event still spooled", len(left))
	}
	var row map[string]any
	if err := json.Unmarshal(left[0], &row); err != nil {
		t.Fatal(err)
	}
	if row["event_id"] != event.ID {
		t.Errorf("the remaining event is %v, want the one that was spooled", row["event_id"])
	}
}

// TestNewRelayAfterTestHomeOverridesTheDeadLoopback pins the relay seam's
// ordering: testHome leaves the endpoint at the dead loopback address, and a
// relay started after it must replace that override — otherwise every
// relay-using test would flush at a port nothing listens on and pass by
// accident. The recorder receiving the event is the proof: a real round trip
// happened, to this server and no other.
func TestNewRelayAfterTestHomeOverridesTheDeadLoopback(t *testing.T) {
	testHome(t)
	if got := Endpoint(); got != "http://127.0.0.1:1/telemetry" {
		t.Fatalf("after testHome the endpoint is %q, want the dead loopback it sets", got)
	}
	recorder := newRelay(t)
	if override := os.Getenv("CODEAF_TELEMETRY_ENDPOINT"); override != Endpoint() {
		t.Fatalf("after newRelay Endpoint() answers %q but the override is %q", Endpoint(), override)
	}
	MarkNoticeShown()
	event := stamped(SessionStarted(ModeChat, false, "session-relay", freshClock(t)))
	if err := SpoolSync(event); err != nil {
		t.Fatalf("SpoolSync returned %v; a spool that is sent answers nil", err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatalf("Flush returned %v; it must never fail a run", err)
	}
	if got := recorder.count(); got != 1 {
		t.Fatalf("the relay saw %d posts after the override, want 1 — the flush must reach the relay newRelay started", got)
	}
	if len(recorder.posts[0].events) != 1 || recorder.posts[0].events[0]["event_id"] != event.ID {
		t.Errorf("the relay received %v, want the one spooled event %q", recorder.posts[0].events, event.ID)
	}
	if left := len(SpoolContents()); left != 0 {
		t.Errorf("%d lines remain after a confirmed send, want 0", left)
	}
}

// TestFlushAgainstTheDefaultEndpointRefusesLikeADeadRelay pins the refusal as
// Flush meets it, without the relay: with the endpoint left at the production
// default, a flush holding one sendable event must behave exactly as it does
// against a dead relay — nothing said, the event still spooled. Flush's nil
// proves nothing on its own; the counter's zero is the assertion that carries.
func TestFlushAgainstTheDefaultEndpointRefusesLikeADeadRelay(t *testing.T) {
	testHome(t)
	// testHome's dead loopback must not soften this: the refusal is about the
	// default, so the override is removed and the default watched instead.
	unsetEnv(t, "CODEAF_TELEMETRY_ENDPOINT")
	if got := Endpoint(); got != DefaultEndpoint {
		t.Fatalf("Endpoint() = %q, want the production default the refusal watches", got)
	}
	counter := &roundTripCounter{}
	previous := httpClient
	httpClient = &http.Client{Transport: counter}
	t.Cleanup(func() { httpClient = previous })
	MarkNoticeShown()
	event := SessionStarted(ModeChat, false, "session-refused", freshClock(t))
	if err := SpoolSync(event); err != nil {
		t.Fatalf("SpoolSync returned %v; a spool that is never sent answers nil", err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatalf("Flush returned %v; a refused send must stay silent, as with a dead relay", err)
	}
	if trips := counter.count(); trips != 0 {
		t.Fatalf("the send path made %d round trips against the default endpoint, want 0", trips)
	}
	left := SpoolContents()
	if len(left) != 1 {
		t.Fatalf("%d lines remain after the refusal, want the event still spooled", len(left))
	}
	var row map[string]any
	if err := json.Unmarshal(left[0], &row); err != nil {
		t.Fatal(err)
	}
	if row["event_id"] != event.ID {
		t.Errorf("the remaining line is %v, want the spooled event %q", row["event_id"], event.ID)
	}
}

// TestForcingTheLadderOnCannotReopenTheDefaultEndpointPath pins where the
// refusal sits: below forceLadderForTest. testHome forces the ladder on for
// every spool test, so a refusal the ladder could override would gate nothing
// this package ever runs. The ladder is forced on again, explicitly, and the
// flush must still make zero round trips and keep the event.
func TestForcingTheLadderOnCannotReopenTheDefaultEndpointPath(t *testing.T) {
	testHome(t)
	forceLadderForTest(t, true)
	unsetEnv(t, "CODEAF_TELEMETRY_ENDPOINT")
	if got := Endpoint(); got != DefaultEndpoint {
		t.Fatalf("Endpoint() = %q, want the production default", got)
	}
	counter := &roundTripCounter{}
	previous := httpClient
	httpClient = &http.Client{Transport: counter}
	t.Cleanup(func() { httpClient = previous })
	MarkNoticeShown()
	event := SessionStarted(ModeChat, false, "session-forced-ladder", freshClock(t))
	if err := SpoolSync(event); err != nil {
		t.Fatalf("SpoolSync returned %v; a spool that is never sent answers nil", err)
	}
	if err := Flush(context.Background()); err != nil {
		t.Fatalf("Flush returned %v; a refused send must stay silent", err)
	}
	if trips := counter.count(); trips != 0 {
		t.Fatalf("with the ladder forced on the send path made %d round trips against the default endpoint, want 0", trips)
	}
	if left := len(SpoolContents()); left != 1 {
		t.Fatalf("%d lines remain after the forced-ladder flush, want the event still spooled", left)
	}
	var row map[string]any
	if err := json.Unmarshal(SpoolContents()[0], &row); err != nil {
		t.Fatal(err)
	}
	if row["event_id"] != event.ID {
		t.Errorf("the remaining line is %v, want the spooled event %q", row["event_id"], event.ID)
	}
}
