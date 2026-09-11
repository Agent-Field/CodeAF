package session

// StillGoing reports that an unattended run has something left to happen.
// A session somebody is steering, a run with no budget, and a task's own agent
// all answer false because none of them has a door that should wait here.
func (a *Agent) StillGoing() bool {
	if a == nil || a.steward() == nil {
		return false
	}

	// THE SESSION LOCK COMES FIRST. The running turn, a queued wake, and the
	// graph's flight are one reading: letting either lock go between them would
	// let a task be admitted in the gap and let the door go home over it. This
	// is the package's lock order — a.mu before graph.mu — and deliberately
	// never calls a method that would take a.mu while the graph is held.
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	queuedWake := false
	for _, note := range a.steering {
		if note.wake {
			queuedWake = true
			break
		}
	}
	if a.running {
		return true
	}
	// A QUEUED WAKE IS WORK STILL TO COME ONLY WHILE A WAKE COULD STILL HAPPEN.
	// [Agent.wakeLocked] refuses to start one past the wall, past the
	// conversation's spending rail, and after the work was stopped — and each of
	// those three is permanent, so a note held behind one of them is not a run
	// still going: it is the sentence the NEXT process says ([durableDelivery]).
	// Its other refusals are either transient (a turn already running is answered
	// on the line above) or already answered here (a closed session, a task's own
	// agent, which holds no steward).
	if queuedWake && !wallIsUp(a.steward()) && !a.workStopped && a.railBlockLocked() == nil {
		return true
	}
	// A SESSION THAT NEVER GROOMED A TASK HAS NO GRAPH, and "nothing is moving" is
	// the honest answer rather than a graph built to answer one question
	// ([Agent.tasker] makes the same argument). The field is read directly for the
	// reason [Agent.Close] reads it directly: building one here would be a door
	// opened by the reading that was only ever asking whether to close.
	graph := a.tasks
	if graph == nil {
		return false
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	return len(graph.flightLocked().moving) > 0
}
