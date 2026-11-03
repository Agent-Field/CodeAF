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
	idle := a.opened && !a.closed && !a.running && len(a.steering) == 0 && len(a.followups) == 0 && len(a.ambient) == 0 && len(a.settling) == 0 && !a.jobWakePending
	graph, jobs := a.tasks, a.jobs
	if a.config.tasker != nil {
		graph = a.config.tasker
	}
	a.mu.Unlock()
	if !idle || a.taskStarting.Load() != 0 || a.titleJobs.pending.Load() != 0 || a.memoryJobs.pending.Load() != 0 || a.orchestrateWorkers.pending.Load() != 0 {
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
	if jobs != nil {
		if !jobs.mu.TryLock() {
			return false
		}
		for _, job := range jobs.jobs {
			select {
			case <-job.done:
			default:
				jobs.mu.Unlock()
				return false
			}
		}
		jobs.mu.Unlock()
	}
	// Readiness must never join deferred disk work on an admission boundary.
	if !a.metaStamp().idle() || !a.modelStamp().idle() || !a.toldStamp().idle() || !a.treesAhead().Idle() {
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
