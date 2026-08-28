package provider

import "testing"

// BenchmarkBabbleGuard is the degeneration guard at DELTA CADENCE, which is
// where it actually runs: once per streamed content delta, inside the read loop,
// against the connection's idle watchdog. The window is deliberately full and
// innocent, so every test runs both halves to completion rather than bailing on
// a short window — the worst case, and the only one worth timing.
func BenchmarkBabbleGuard(b *testing.B) {
	prose := plainProse(400)
	deltas := make([]string, 0, 64)
	for len(prose) > 0 {
		at := 200
		if at > len(prose) {
			at = len(prose)
		}
		deltas = append(deltas, prose[:at])
		prose = prose[at:]
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		watch := &babbleWatch{}
		for _, delta := range deltas {
			if watch.write(delta) {
				b.Fatal("innocent prose tripped the guard")
			}
		}
	}
}
