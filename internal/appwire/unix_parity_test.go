package appwire

import (
	"encoding/json"
	"testing"
)

// Unix parity contract 3: the old setup and health payloads still read and
// round-trip with the same fields and values. Shared new facts are optional.
// Unix parity 2 contract items 1/2 retain this legacy wire contract.
func TestUnixParityLegacyWire(t *testing.T) {
	for _, fixture := range []struct {
		raw   string
		value any
	}{
		{`{"step":2,"provider":"openrouter","default_model":"fixture/model","day_cap_usd":7,"start":"chat"}`, &Setup{}},
		{`{"version":"fixture","started_at":"2026-01-01T00:00:00Z","uptime_s":3,"disks":[]}`, &Health{}},
	} {
		if err := json.Unmarshal([]byte(fixture.raw), fixture.value); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(fixture.value)
		if err != nil || string(raw) != fixture.raw {
			t.Fatalf("legacy payload = %s, %v; want %s", raw, err, fixture.raw)
		}
	}
}
