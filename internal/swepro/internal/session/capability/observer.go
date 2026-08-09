package capability

import (
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"
)

// OutcomeObserver serializes scheduler outcome delivery into a tracker.
type OutcomeObserver struct {
	mu      sync.Mutex
	tracker *CapabilityTracker
}

func NewOutcomeObserver(tracker *CapabilityTracker) *OutcomeObserver {
	return &OutcomeObserver{tracker: tracker}
}

func (observer *OutcomeObserver) Observe(outcome leafoutcome.LeafOutcome) {
	if observer == nil || observer.tracker == nil {
		return
	}
	observer.mu.Lock()
	observer.tracker.Observe(FromLeafOutcome(outcome))
	observer.mu.Unlock()
}
