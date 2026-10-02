package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

func TestSettingsTelemetryOffKeepsQueuedCountsUnsentThroughExit(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "")
	t.Setenv("DO_NOT_TRACK", "")
	var requests atomic.Int64
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(relay.Close)
	t.Setenv("CODEAF_TELEMETRY_ENDPOINT", relay.URL)
	telemetry.Configure(false)
	t.Cleanup(func() { telemetry.Configure(false) })
	telemetry.VersionForTest(t, "v0.0.0-test")
	telemetry.ResetCountersForTest(t)
	telemetry.EnableForTest(t, true)
	if err := telemetry.SpoolSync(telemetry.UsageDelta(telemetry.ModeChat, 10, 5, "open-session", time.Now())); err != nil {
		t.Fatal(err)
	}
	session := telemetryStart(telemetrySession{mode: telemetry.ModeChat, sessionID: "open-session"})
	t.Cleanup(session.stopPeriodicFlush)
	t.Cleanup(session.finishUsage)
	before := len(telemetrySpoolRows(t))
	if before != 1 {
		t.Fatalf("fixture spooled %d events, want one waiting to leave", before)
	}
	// The test override bypasses every opt-out. Use it only to seed the open
	// session, then read the real ladder so the registry's live answer is tested.
	telemetry.EnableForTest(t, false)
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()}).Row(config.KeyTelemetry)
	if !ok {
		t.Fatal("telemetry switch is missing")
	}
	if err := row.Apply("off"); err != nil {
		t.Fatal(err)
	}
	if telemetry.Enabled() || telemetry.OffReason() != telemetry.OffConfig {
		t.Fatalf("settings off left the live gate at %q", telemetry.OffReason())
	}
	telemetry.CountTokens(100, 25)
	telemetry.Spool(telemetry.UsageDelta(telemetry.ModeChat, 100, 25, session.sessionID, time.Now()))
	_ = telemetry.Flush(context.Background())
	telemetryEnd(session, 0)
	if after := len(telemetrySpoolRows(t)); after != before || requests.Load() != 0 {
		t.Fatalf("off sent or appended events through exit: rows=%d (was %d) requests=%d", after, before, requests.Load())
	}
	for _, row := range telemetrySpoolRows(t) {
		if row["event_name"] == "session_ended" {
			t.Fatal("off built a session-ended event")
		}
	}
}
