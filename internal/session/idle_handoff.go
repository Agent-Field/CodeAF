package session

import (
	"sync"
	"sync/atomic"
)

// sessionJobs retains the existing join while allowing an idle handoff to
// observe writers without waiting for a model or adding a waiter goroutine.
type sessionJobs struct {
	sync.WaitGroup
	pending atomic.Int64
}

func (j *sessionJobs) Add(n int) {
	j.pending.Add(int64(n))
	j.WaitGroup.Add(n)
}

func (j *sessionJobs) Done() {
	j.WaitGroup.Done()
	j.pending.Add(-1)
}

// idleForHandoff is conservative while any activity lock is busy. Admission
// calls the ownership gate before recording work; refusing a busy snapshot
// avoids taking that gate's lock in the opposite order to a session writer.
func (a *Agent) idleForHandoff() bool {
	if !a.mu.TryLock() {
		return false
	}
	// Ambient watch progress is only next-turn context. The job's log keeps
	// it, and an active watch still blocks handoff through its job entry.
	idle := a.opened && !a.closed && !a.running && len(a.steering) == 0 && len(a.followups) == 0 && len(a.settling) == 0 && !a.jobWakePending
	// Read jobs while their replies cannot enter the agent's queues. An exit
	// that closed done but still owes a callback stays in delivering.
	if idle && a.jobs != nil {
		idle = a.jobs.idleForHandoff()
	}
	graph := a.tasks
	if a.config.tasker != nil {
		graph = a.config.tasker
	}
	a.mu.Unlock()
	if !idle || a.settingsWrites.Load() != 0 || a.taskStarting.Load() != 0 || a.titleJobs.pending.Load() != 0 || a.memoryJobs.pending.Load() != 0 || a.orchestrateWorkers.pending.Load() != 0 {
		return false
	}
	if !a.beltStartMu.TryLock() {
		return false
	}
	a.beltStartMu.Unlock()
	if !a.beltMu.TryLock() {
		return false
	}
	belt := a.beltRun != nil
	a.beltMu.Unlock()
	if belt {
		return false
	}
	if graph != nil {
		if !graph.mu.TryLock() {
			return false
		}
		idle = graph.runners.pending.Load() == 0
		for _, node := range graph.nodes {
			if node != nil && !node.state.settled() {
				idle = false
				break
			}
		}
		for _, row := range graph.runRowsLocked() {
			if !row.State.settled() {
				idle = false
				break
			}
		}
		graph.mu.Unlock()
		if !idle {
			return false
		}
	}
	// Readiness must never join deferred disk work on an admission boundary.
	if !a.metaStamp().idle() || !a.modelStamp().idle() || !a.spendStamp().idle() || !a.toldStamp().idle() || !a.treesAhead().Idle() {
		return false
	}
	for _, store := range []*fixStore{a.fixShelfFor().project, a.fixShelfFor().global} {
		if store != nil && !store.counters().Idle() {
			return false
		}
	}
	lane := &a.team.events
	if !lane.mu.TryLock() {
		return false
	}
	defer lane.mu.Unlock()
	return len(lane.queue) == 0 && lane.done == nil
}

func (r *jobRegistry) idleForHandoff() bool {
	if !r.mu.TryLock() {
		return false
	}
	defer r.mu.Unlock()
	for _, job := range r.jobs {
		select {
		case <-job.done:
		default:
			return false
		}
	}
	// Settlement is registered before done closes, so check it after all the
	// channels read as closed.
	return r.delivering.Load() == 0
}
