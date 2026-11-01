package appwire

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type healthEngine struct {
	sendEngine
	health Health
}

func (h *healthEngine) HealthCheck(context.Context) (Health, error) { return h.health, nil }

// health.check answers the engine's facts as it gave them, and an engine with
// no health door answers unknown_method.
func TestHealthCheckAnswersTheEnginesFacts(t *testing.T) {
	check := methods["health.check"]
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	eng := &healthEngine{health: Health{Version: "v1", StartedAt: at, UptimeS: 60, Disks: []HealthDisk{{Role: DiskWorkFolder, Path: "/w", FreeBytes: 1, TotalBytes: 2}}}}
	r, err := check.call(eng, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	got := r.(*HealthResult).Health
	if got.Version != "v1" || got.UptimeS != 60 || len(got.Disks) != 1 || got.Disks[0].TotalBytes != 2 {
		t.Fatalf("answered %+v", got)
	}
	if _, err := check.call(&sendEngine{}, json.RawMessage(`{}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatalf("an engine without the door answered %v", err)
	}
}
