package session

func hubRunID(h *eventHub) string {
	if h == nil {
		return ""
	}
	return h.runID
}

// Background notifications carry their own provenance; receiving them during
// this turn never makes them work of this turn.
func foregroundRunEvent(kind EventKind) bool {
	switch kind {
	case EventTextDelta, EventThinking, EventReasoning, EventToolBegin,
		EventToolEnd, EventToolFailed, EventToolOutput, EventToolAnnounced, EventCloseContinued,
		EventToolForming, EventToolFinished, EventTurnDone:
		return true
	}
	return false
}
