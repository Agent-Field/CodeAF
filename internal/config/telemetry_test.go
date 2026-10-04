package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/telemetry"
)

func TestTelemetryRegistryOffStopsTheRunningProcessAndOnWaitsForRestart(t *testing.T) {
	t.Setenv("CODEAF_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", "http://127.0.0.1:1/telemetry")
	telemetry.EnableForTest(t, false)
	telemetry.Configure(false)
	t.Cleanup(func() { telemetry.Configure(false) })
	row, ok := registry(t, t.TempDir()).Row(KeyTelemetry)
	if !ok {
		t.Fatal("telemetry switch is missing")
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if telemetry.OffReason() != telemetry.OffConfig || telemetry.Enabled() {
		t.Fatalf("registry off left the running gate at %q, want %q", telemetry.OffReason(), telemetry.OffConfig)
	}
	if err := row.Apply("on"); err != nil {
		t.Fatal(err)
	}
	if row.Value() != "on" || telemetry.OffReason() != telemetry.OffConfig {
		t.Fatalf("on must persist for next start without reopening this process: row=%q gate=%q", row.Value(), telemetry.OffReason())
	}
}

// The telemetry row is registered the way the call-log and history rows are:
// a key, a default, an environment pin, a place in the sheet, and a reader
// the binary can call. These tests hold that shape still, because the
// completeness gate in settings_test.go only fails when a pin is MISSING from
// the registry — it says nothing about a row that reads the wrong value.

func writeTelemetryProfile(t *testing.T, dir string, on bool) {
	t.Helper()
	if err := writeBool(dir, KeyTelemetry, formatBool(on)); err != nil {
		t.Fatalf("writeBool: %v", err)
	}
}

func TestTelemetryRowIsRegisteredLikeItsNeighbours(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_TELEMETRY", "")
	row, found := registry(t, dir).Row(KeyTelemetry)
	if !found {
		t.Fatal("the telemetry row is not registered")
	}
	if row.Key != KeyTelemetry || row.Label != "telemetry" || row.Env != "CODEAF_TELEMETRY" {
		t.Errorf("telemetry row = %+v", row)
	}
	if got := row.Value(); got != "on" {
		t.Errorf("an unset row reads %q, want on (the default)", got)
	}
	if row.Accepts() != "on or off" {
		t.Errorf("the row accepts %q, want on or off", row.Accepts())
	}

	// The row writes, and the write lands where the reader looks.
	if err := row.Apply("off"); err != nil {
		t.Fatalf("Apply(off): %v", err)
	}
	if got := TelemetryAt(dir); got {
		t.Fatal("after Apply(off) the row still reads on")
	}

	// A pinned row refuses calmly, the way every other pinned row does.
	t.Setenv("CODEAF_TELEMETRY", "off")
	if err := row.Apply("on"); err == nil {
		t.Fatal("a pinned row accepted a write")
	}
	if _, reason := TelemetryOffReason("", dir); reason == "" {
		t.Fatal("the off reason does not name the pin")
	}
}

func TestTelemetryAtWalksTheLadder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_TELEMETRY", "")

	if !TelemetryAt(dir) {
		t.Fatal("default is on")
	}

	writeTelemetryProfile(t, dir, false)
	if TelemetryAt(dir) {
		t.Fatal("the profile config did not turn telemetry off")
	}
	if off, reason := TelemetryOffReason("", dir); !off || reason == "" {
		t.Fatalf("off = %v, reason = %q, want the profile config named", off, reason)
	}

	// The environment wins over the profile.
	t.Setenv("CODEAF_TELEMETRY", "on")
	if !TelemetryAt(dir) {
		t.Fatal("the pin did not win over the profile config")
	}
	if off, _ := TelemetryOffReason("", dir); off {
		t.Fatal("an on pin reports itself off")
	}
}

func TestTelemetryAtReadsTheProjectConfig(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	t.Setenv("CODEAF_TELEMETRY", "")

	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	path := ProjectConfigPath(work)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"telemetry": false}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if TelemetryAtIn(work, dir) {
		t.Fatal("the project config did not turn telemetry off")
	}
	if off, reason := TelemetryOffReason(work, dir); !off || reason == "" {
		t.Fatalf("off = %v, reason = %q, want the project file named", off, reason)
	}

	// The pin outranks the project file, and a profile off behind an
	// unpinned project off is still off.
	t.Setenv("CODEAF_TELEMETRY", "on")
	if !TelemetryAtIn(work, dir) {
		t.Fatal("the pin did not outrank the project file")
	}
	t.Setenv("CODEAF_TELEMETRY", "")
	writeTelemetryProfile(t, dir, true)
	if TelemetryAtIn(work, dir) {
		t.Fatal("a profile on did not restore telemetry under a project off")
	}

	// A malformed value is nobody's answer, and the row reads as if the
	// line were not there.
	if err := os.WriteFile(path, []byte(`{"telemetry": "maybe"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !TelemetryAtIn(work, dir) {
		t.Fatal("a malformed project value turned telemetry off")
	}
}

// The counters-whether-or-not rule's config half: the accessor answers
// whether the pipe is off without ever claiming the counters stopped.
func TestTelemetryOffReasonAnswersNothingWhenOn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_TELEMETRY", "")
	writeTelemetryProfile(t, dir, true)
	if off, reason := TelemetryOffReason("", dir); off || reason != "" {
		t.Fatalf("off = %v, reason = %q, want both empty", off, reason)
	}
}
