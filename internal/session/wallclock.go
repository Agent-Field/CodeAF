package session

// THE WALL READER OUTSIDE A TURN.
//
// Three unattended canary cells handed their work to a task, went idle, and
// were still sitting at `1 running` when the rig killed them at 902 seconds
// inside an 840-second wall. The only reader of the wall was Steward.Decide,
// and that road is reached only at the end of a turn; an idle conversation with
// work out has no turn ending to reach it.
//
// THE WALL IS READ BY SOMETHING THAT IS NOT THE END OF A TURN. This file adds
// that one reader. It waits without calling a model, lets a speaking turn reach
// its own ending, and acts only while this session still has moving work. The
// ending remains the budget's own [stopSpent], and work is stopped through the
// same [TaskGraph.stop] road as a person's stop so its branch and working copy
// stay where they are.

import "time"

// wallSettleTick is how often a passed wall re-asks the two facts that have no
// channel of their own: whether a turn finished and whether work is still out.
// One second is short beside a stated wall and keeps an idle goroutine from
// spinning while a speaking turn winds up.
const wallSettleTick = time.Second

// wallIsUp answers the wall ALONE — no money, no spend closure — because its
// one caller holds the session's lock that the closure would take
// ([Agent.wakeLocked]). It reads the Steward's own wall and its own start, so
// there is no second statement of either anywhere in the build.
func wallIsUp(steward *Steward) bool {
	if steward == nil {
		return false
	}
	steward.mu.Lock()
	wall, started, now := steward.wall, steward.started, steward.now
	steward.mu.Unlock()
	return wall > 0 && !started.IsZero() && now().Sub(started) >= wall
}

// stopStoppedMovingTail tells the person what became of work the wall found in
// flight. It is separate from stopLeftItMovingTail because that turn-ending
// road leaves work alone, while this reader stops it and keeps what it did.
const stopStoppedMovingTail = " · work was still going, so it was stopped and what it did was kept"

// armWallClock starts the one reader only for an unattended session whose
// budget names a wall. A person, an unattended session with no budget, and a
// money-only ceiling get no channel and no goroutine at all.
func (a *Agent) armWallClock() {
	steward := a.steward()
	if steward == nil || steward.Budget().Wall <= 0 {
		return
	}
	stop := make(chan struct{})
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.wallStop = stop
	a.mu.Unlock()
	go a.watchTheWall(steward, stop)
}

// watchTheWall sleeps once until the wall and polls only after it. The wait is
// derived from Steward.Budget so tests and the running session share the one
// clock the Steward owns; Close ends either wait through stop.
func (a *Agent) watchTheWall(steward *Steward, stop chan struct{}) {
	left, _ := steward.Budget().Left()
	timer := time.NewTimer(left)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stop:
		return
	}
	for {
		// The first reading is one settle tick PAST the wall, like every reading
		// after it. A turn or frontier transition already beginning at the edge
		// can become visible in that interval, so the reader observes a settled
		// session fact rather than racing the timer's own instant.
		timer.Reset(wallSettleTick)
		select {
		case <-timer.C:
		case <-stop:
			return
		}
		if a.endRunOnTheWall() {
			return
		}
	}
}

// endRunOnTheWall takes the budget's ending once moving work is exposed on an
// idle session. True means the reader is finished; false means the turn or the
// work state can still change and should be asked again.
func (a *Agent) endRunOnTheWall() bool {
	steward := a.steward()
	if steward == nil {
		return true
	}
	spent, why := steward.Budget().Exhausted()
	if !spent {
		return false
	}

	a.mu.Lock()
	switch {
	case a.closed:
		a.mu.Unlock()
		return true
	case a.running:
		a.mu.Unlock()
		return false
	case a.wallEndingTaken:
		a.mu.Unlock()
		return true
	}
	a.mu.Unlock()

	graph := a.tasker()
	if graph == nil {
		return false
	}
	graph.mu.Lock()
	flight := graph.flightLocked()
	graph.mu.Unlock()
	if len(flight.moving) == 0 {
		return false
	}

	// The state may have changed while the graph was read. Re-read the session
	// under its own lock before taking the ending, without ever nesting that
	// lock with the graph's.
	a.mu.Lock()
	switch {
	case a.closed:
		a.mu.Unlock()
		return true
	case a.running:
		a.mu.Unlock()
		return false
	case a.wallEndingTaken:
		a.mu.Unlock()
		return true
	}
	a.wallEndingTaken = true
	a.mu.Unlock()

	a.stopWorkOnTheWall()
	decision := stopSpent(why)
	if len(flight.moving) > 0 {
		decision.Reason += stopStoppedMovingTail
	}
	note := checkpointStoppedNote + decision.Reason
	a.journalDecision(decision)
	a.record(textMessage("assistant", note))

	// A wall ending is a notice on each standing lane, not a turn. The stream is
	// handed over without waiting, carries one event, and closes; no provider is
	// called and nothing is put on the turn queue.
	a.mu.Lock()
	for _, lane := range a.wakeLanes {
		stream := newEventStream()
		select {
		case lane <- stream.out:
			stream.send(Event{Kind: EventNotice, Text: note})
			stream.close()
		default:
			stream.close()
		}
	}
	a.mu.Unlock()
	return true
}

// stopWorkOnTheWall closes the graph's frontier before stopping every
// unsettled node through TaskGraph.stop. That road settles queued work at once,
// lets a running worker land its stopped row, and deliberately leaves every
// branch and working copy alone.
func (a *Agent) stopWorkOnTheWall() {
	graph := a.tasker()
	if graph == nil {
		return
	}
	graph.mu.Lock()
	graph.quitting = true
	ids := make([]uint64, 0, len(graph.order))
	for _, id := range graph.order {
		if node := graph.nodes[id]; node != nil && !node.state.settled() {
			ids = append(ids, id)
		}
	}
	graph.mu.Unlock()

	for _, id := range ids {
		_, _ = graph.stop(id)
	}
}
