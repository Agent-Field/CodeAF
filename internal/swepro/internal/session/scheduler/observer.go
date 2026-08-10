package scheduler

import "github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/leafoutcome"

// OutcomeObserverFunc adapts a function to OutcomeObserver.
type OutcomeObserverFunc func(leafoutcome.LeafOutcome)

func (observe OutcomeObserverFunc) Observe(outcome leafoutcome.LeafOutcome) {
	observe(outcome)
}

type broadcastOutcomeObserver []OutcomeObserver

func (observers broadcastOutcomeObserver) Observe(outcome leafoutcome.LeafOutcome) {
	for _, observer := range observers {
		observer.Observe(outcome)
	}
}

// BroadcastOutcomeObservers combines non-nil observers. A single observer is
// returned unchanged so existing one-observer behavior has no extra layer.
func BroadcastOutcomeObservers(observers ...OutcomeObserver) OutcomeObserver {
	filtered := make([]OutcomeObserver, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			filtered = append(filtered, observer)
		}
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return broadcastOutcomeObserver(filtered)
	}
}
