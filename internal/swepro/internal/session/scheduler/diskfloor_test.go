package scheduler

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/isolation"
)

// stubVolume replaces the volume measurement for one test.
func stubVolume(t *testing.T, totalGB float64, ok bool) {
	t.Helper()
	previous := volumeTotalGB
	volumeTotalGB = func(string) (float64, bool) { return totalGB, ok }
	t.Cleanup(func() { volumeTotalGB = previous })
}

func TestCapDiskFloorGB(t *testing.T) {
	tests := []struct {
		name       string
		floor      float64
		totalGB    float64
		measurable bool
		want       float64
	}{
		// The bug: a 5GB floor on a 5GB volume pauses below 2.5GB free and can
		// never clear, because the volume cannot hold a floor its own size.
		{name: "small volume", floor: 5, totalGB: 5, measurable: true, want: 0.5},
		{name: "large volume keeps the ported floor", floor: 5, totalGB: 500, measurable: true, want: 5},
		{name: "unmeasurable volume keeps the floor", floor: 5, measurable: false, want: 5},
		{name: "disabled guard stays disabled", floor: 0, totalGB: 5, measurable: true, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubVolume(t, tc.totalGB, tc.measurable)
			if got := capDiskFloorGB("/workspace", tc.floor); got != tc.want {
				t.Fatalf("capDiskFloorGB(%v, total=%v) = %v, want %v",
					tc.floor, tc.totalGB, got, tc.want)
			}
		})
	}
}

// The whole point of the cap is the dispatch decision it changes: on a small
// volume a reading that used to pause forever now proceeds, and one that is
// genuinely out of space still pauses.
func TestGatherEnvelopeReadingsCapsFloorToVolume(t *testing.T) {
	stubVolume(t, 5, true)
	tests := []struct {
		name    string
		freeGB  float64
		floorGB float64
		status  isolation.DispatchEnvelopeStatus
	}{
		{name: "unclearable pause proceeds", freeGB: 2, floorGB: 5, status: isolation.StatusProceed},
		{name: "genuinely low serializes", freeGB: 0.4, floorGB: 5, status: isolation.StatusSerialize},
		{name: "genuinely empty pauses", freeGB: 0.1, floorGB: 5, status: isolation.StatusPause},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			readings := gatherEnvelopeReadings("/workspace", fakeEnvelopeReader{
				envelope: diskEnvelope(tc.freeGB, tc.floorGB, false),
			})
			if len(readings) != 1 {
				t.Fatalf("readings = %#v, want one", readings)
			}
			if float64(readings[0].FloorGB) != 0.5 {
				t.Fatalf("floor = %v, want the volume-relative 0.5", readings[0].FloorGB)
			}
			if got := isolation.DecideDispatchEnvelope(readings).Status; got != tc.status {
				t.Fatalf("status = %q, want %q (reading %#v)", got, tc.status, readings[0])
			}
		})
	}
}

// A healthy reading is passed through untouched and never pays for a
// measurement it cannot be wrong about.
func TestGatherEnvelopeReadingsLeavesHealthyReadingsAlone(t *testing.T) {
	measured := false
	previous := volumeTotalGB
	volumeTotalGB = func(string) (float64, bool) { measured = true; return 5, true }
	t.Cleanup(func() { volumeTotalGB = previous })

	readings := gatherEnvelopeReadings("/workspace", fakeEnvelopeReader{
		envelope: diskEnvelope(400, 5, true),
	})
	if len(readings) != 1 || !readings[0].OK || float64(readings[0].FloorGB) != 5 {
		t.Fatalf("healthy reading = %#v", readings)
	}
	if measured {
		t.Fatal("a healthy reading measured the volume it did not need")
	}
}

// The real measurement, once, against the volume this test is running on: it
// must produce a positive size and cache it, and a path that does not exist
// must report unmeasurable rather than a zero that would disable the floor.
func TestVolumeTotalGBMeasuresTheRealVolume(t *testing.T) {
	directory := t.TempDir()
	total, ok := cachedVolumeTotalGB(directory)
	if !ok || total <= 0 {
		t.Skipf("volume size unavailable on this platform: %v, %t", total, ok)
	}
	cached, ok := volumeTotals.Load(directory)
	if !ok || cached.(float64) != total {
		t.Fatalf("measurement was not cached: %v", cached)
	}
	if _, ok := cachedVolumeTotalGB(directory + "/does/not/exist"); ok {
		t.Fatal("an unstattable path reported a volume size")
	}
}
