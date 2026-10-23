package session

import "github.com/Agent-Field/codeaf/internal/router"

// A request waiting for the start lock belongs to the admission epoch in which
// it arrived. A later explicit Submit cannot revive a request cut by Stop.
func (a *Agent) beltAdmissionEpoch() (uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return 0, errAgentClosed
	}
	if a.workStopped {
		return 0, errWorkStopping
	}
	return a.workEpoch, nil
}
func (a *Agent) checkBeltAdmission(epoch uint64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errAgentClosed
	}
	if a.workStopped || a.workEpoch != epoch {
		return errWorkStopping
	}
	return nil
}

// This is the last cancellable boundary before the existing atomic home merge.
// A stop that won earlier keeps the isolated copy. Once homecoming has begun,
// its actual receipt must survive; Stop only suppresses subsequent model work.
func (a *Agent) beginBeltHomecoming(run *beltRun) bool {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	if run.stopped {
		return false
	}
	run.bringingHome = true
	return true
}
func (a *Agent) beltRunQuiet(run *beltRun) bool {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	return run.quiet
}
func (a *Agent) settleBeltStopIfNeeded(run *beltRun, summary RunSummary) bool {
	if stopped, why := a.beltRunStopped(run); stopped {
		a.settleStoppedBeltRun(run, why, summary.Cut)
		a.settleTaskCrew(run.row, router.CrewStopped, summary.USD)
		return true
	}
	return false
}
