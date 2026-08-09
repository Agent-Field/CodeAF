// This file ports src/session/plandb-scheduler.ts:126-144 and 1690-1725 from
// swe-pro (commit 3b25a1a).
package scheduler

import (
	"fmt"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/aimd"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/isolation"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/resourceguard"
)

type resourceGuardReader struct{}

func (resourceGuardReader) CheckDiskEnvelope(path string) (resourceguard.DiskEnvelope, error) {
	return resourceguard.CheckDiskEnvelope(path), nil
}

// gatherEnvelopeReadings is gatherDispatchEnvelope
// (plandb-scheduler.ts:126-144). It is deliberately a list-valued seam and a
// catch-all: an error or panic produces no readings, which decides "proceed."
func gatherEnvelopeReadings(path string, reader diskEnvelopeReader) (readings []isolation.EnvelopeReading) {
	if reader == nil {
		reader = resourceGuardReader{}
	}
	defer func() {
		if recover() != nil {
			readings = []isolation.EnvelopeReading{}
		}
	}()
	disk, err := reader.CheckDiskEnvelope(path)
	if err != nil {
		return []isolation.EnvelopeReading{}
	}
	return []isolation.EnvelopeReading{{
		Resource:   "disk",
		OK:         disk.OK,
		HeadroomGB: jscompat.JSNumber(disk.FreeGB),
		FloorGB:    jscompat.JSNumber(disk.FloorGB),
	}}
}

type lockedAimd struct {
	mu         sync.Mutex
	controller *aimd.AimdController
}

func (a *lockedAimd) window() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.controller.Window()
}

func (a *lockedAimd) observe(mergeOK bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.controller.Observe(mergeOK)
}

type aimdControllerRegistry struct {
	mu      sync.Mutex
	bySpace map[string]*lockedAimd
}

func newAimdControllerRegistry() *aimdControllerRegistry {
	return &aimdControllerRegistry{bySpace: map[string]*lockedAimd{}}
}

func (r *aimdControllerRegistry) loadOrCreate(workspace string, maxParallel float64) (*lockedAimd, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if controller := r.bySpace[workspace]; controller != nil {
		return controller, nil
	}
	capacity := maxParallel * 2
	floor := 1.0
	controller, err := aimd.CreateAimdController(&aimd.AimdOptions{
		InitialWindow: &maxParallel,
		Cap:           &capacity,
		Floor:         &floor,
	})
	if err != nil {
		// The TS construction site has no catch. Returning this error lets the
		// phase-two cycle surface the same whole-cycle failure.
		return nil, err
	}
	locked := &lockedAimd{controller: controller}
	r.bySpace[workspace] = locked
	return locked, nil
}

var schedulerAimdControllers = newAimdControllerRegistry()

type dispatchEnvelope struct {
	Readings       []isolation.EnvelopeReading
	Decision       isolation.DispatchEnvelopeDecision
	Status         isolation.DispatchEnvelopeStatus
	BaseWindow     float64
	ParallelWindow float64
	Aimd           *lockedAimd
}

// deriveDispatchEnvelope ports the AIMD and resource-envelope computation at
// plandb-scheduler.ts:1690-1725. Serialize clamps the effective window to one;
// pause deliberately keeps the base window because the later capacity gate,
// not this derivation, suppresses claims.
func deriveDispatchEnvelope(
	workspace string,
	maxParallel float64,
	adaptiveCuts bool,
	reader diskEnvelopeReader,
) (dispatchEnvelope, error) {
	baseWindow := maxParallel
	var controller *lockedAimd
	if adaptiveCuts {
		var err error
		controller, err = schedulerAimdControllers.loadOrCreate(workspace, maxParallel)
		if err != nil {
			return dispatchEnvelope{}, fmt.Errorf("create scheduler AIMD controller: %w", err)
		}
		baseWindow = controller.window()
	}

	readings := gatherEnvelopeReadings(workspace, reader)
	decision := isolation.DecideDispatchEnvelope(readings)
	parallelWindow := baseWindow
	if decision.Status == isolation.StatusSerialize {
		parallelWindow = 1
	}
	return dispatchEnvelope{
		Readings:       readings,
		Decision:       decision,
		Status:         decision.Status,
		BaseWindow:     baseWindow,
		ParallelWindow: parallelWindow,
		Aimd:           controller,
	}, nil
}

func schedulerMaxParallel(input SchedulerInput) float64 {
	if input.MaxParallel == nil {
		return defaultMaxParallel
	}
	return *input.MaxParallel
}
