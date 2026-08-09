package scheduler

import (
	"errors"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/isolation"
	"github.com/Agent-Field/swe-pro-go/internal/session/resourceguard"
)

type fakeEnvelopeReader struct {
	envelope resourceguard.DiskEnvelope
	err      error
	panic    bool
}

func (f fakeEnvelopeReader) CheckDiskEnvelope(string) (resourceguard.DiskEnvelope, error) {
	if f.panic {
		panic("measurement panic")
	}
	return f.envelope, f.err
}

func diskEnvelope(free, floor float64, ok bool) resourceguard.DiskEnvelope {
	return resourceguard.DiskEnvelope{
		FreeGB:  jscompat.JSNumber(free),
		FloorGB: jscompat.JSNumber(floor),
		OK:      ok,
	}
}

func TestGatherEnvelopeReadingsFailsOpen(t *testing.T) {
	for _, reader := range []diskEnvelopeReader{
		fakeEnvelopeReader{err: errors.New("unmeasurable")},
		fakeEnvelopeReader{panic: true},
	} {
		if got := gatherEnvelopeReadings("/workspace", reader); len(got) != 0 {
			t.Fatalf("gatherEnvelopeReadings = %#v, want empty", got)
		}
	}
}

func TestDeriveDispatchEnvelope(t *testing.T) {
	schedulerAimdControllers = newAimdControllerRegistry()
	tests := []struct {
		name         string
		disk         resourceguard.DiskEnvelope
		status       isolation.DispatchEnvelopeStatus
		baseWindow   float64
		parallel     float64
		readingCount int
	}{
		{
			name:         "proceed",
			disk:         diskEnvelope(8, 5, true),
			status:       isolation.StatusProceed,
			baseWindow:   6,
			parallel:     6,
			readingCount: 1,
		},
		{
			name:         "serialize",
			disk:         diskEnvelope(3, 5, false),
			status:       isolation.StatusSerialize,
			baseWindow:   6,
			parallel:     1,
			readingCount: 1,
		},
		{
			name:         "pause-keeps-base-window",
			disk:         diskEnvelope(2, 5, false),
			status:       isolation.StatusPause,
			baseWindow:   6,
			parallel:     6,
			readingCount: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deriveDispatchEnvelope(
				t.Name(),
				6,
				false,
				fakeEnvelopeReader{envelope: tc.disk},
			)
			if err != nil {
				t.Fatalf("deriveDispatchEnvelope: %v", err)
			}
			if got.Status != tc.status ||
				got.BaseWindow != tc.baseWindow ||
				got.ParallelWindow != tc.parallel ||
				len(got.Readings) != tc.readingCount {
				t.Fatalf("deriveDispatchEnvelope = %#v", got)
			}
		})
	}
}

func TestDeriveDispatchEnvelopeUsesStickyAimdWindow(t *testing.T) {
	schedulerAimdControllers = newAimdControllerRegistry()
	reader := fakeEnvelopeReader{envelope: diskEnvelope(8, 5, true)}
	first, err := deriveDispatchEnvelope("/same-workspace", 4, true, reader)
	if err != nil {
		t.Fatalf("first derive: %v", err)
	}
	if first.BaseWindow != 4 || first.Aimd == nil {
		t.Fatalf("first = %#v", first)
	}
	first.Aimd.observe(true)

	second, err := deriveDispatchEnvelope("/same-workspace", 99, true, reader)
	if err != nil {
		t.Fatalf("second derive: %v", err)
	}
	// The first caller's {initial:4, cap:8} controller wins for this workspace.
	if second.BaseWindow != 5 || second.ParallelWindow != 5 {
		t.Fatalf("second windows = %v/%v, want 5/5", second.BaseWindow, second.ParallelWindow)
	}
}

func TestDeriveDispatchEnvelopePropagatesAimdConstructionFailure(t *testing.T) {
	schedulerAimdControllers = newAimdControllerRegistry()
	_, err := deriveDispatchEnvelope(
		"/zero-window",
		0,
		true,
		fakeEnvelopeReader{envelope: diskEnvelope(8, 5, true)},
	)
	if err == nil {
		t.Fatal("deriveDispatchEnvelope maxParallel=0 succeeded")
	}
}

func TestSchedulerMaxParallelKeepsExplicitZero(t *testing.T) {
	if got := schedulerMaxParallel(SchedulerInput{}); got != defaultMaxParallel {
		t.Fatalf("default = %v", got)
	}
	zero := 0.0
	if got := schedulerMaxParallel(SchedulerInput{MaxParallel: &zero}); got != 0 {
		t.Fatalf("explicit zero = %v", got)
	}
}
