package stalereaper

import "testing"

func TestKeptPackedTimestampBug(t *testing.T) {
	const (
		now       = 1_770_000_000_000
		threshold = 3_600_000
	)
	packed := float64((now - 2*threshold) * 4096)
	got := LegacyReapSelection([]*LegacyReapTaskRow{
		{ID: "real", LastHeartbeat: packed},
	}, now, threshold)
	if len(got) != 0 {
		t.Fatalf("packed timestamp reaped: %v", got)
	}

	intended := LegacyReapSelection([]*LegacyReapTaskRow{
		{ID: "iso", LastHeartbeat: "2026-02-02T00:40:00.000Z"},
	}, now, threshold)
	if len(intended) != 1 || intended[0] != "iso" {
		t.Fatalf("date-string control path = %v", intended)
	}
}
