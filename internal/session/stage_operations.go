package session

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

// StageCheckProgress identifies finite checks still owned by this stage.
// Ordinary conversation background work retains its existing lifetime.
type StageCheckProgress struct {
	CallID  string
	Command string
	Started time.Time
}

// ArmStageJobPark reuses the task wait without making ordinary chats unattended.
func (a *Agent) ArmStageJobPark(bound time.Duration) {
	if !a.config.mayStage() {
		return
	}
	a.armJobPark(bound)
}

func (j *job) stageOutstanding() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.stageOwned && !j.stageDelivered
}
func (r *jobRegistry) stageRunning() bool {
	if r == nil || !r.stage {
		return false
	}
	for _, j := range r.all() {
		if j.stageOutstanding() {
			return true
		}
	}
	return false
}
func (a *Agent) stageGoingLocked() bool {
	return a.stillGoingLocked() || (!a.closed && a.jobs != nil && a.jobs.stageRunning())
}

// StageStillGoing includes check endings not yet delivered to the stage.
func (a *Agent) StageStillGoing() bool {
	if a == nil {
		return false
	}
	if !a.config.mayStage() {
		return a.StillGoing()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stageGoingLocked()
}

// SettleStage closes automatic wake admission in the same read as quiescence.
// A runner continuation explicitly reopens it after judging the saved report.
func (a *Agent) SettleStage() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stageGoingLocked() {
		return false
	}
	a.stageSettled = true
	return true
}
func (a *Agent) ReopenStage() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stageSettled = false
}

// StageChecks exposes owned check progress, never auxiliary servers or watches.
func (a *Agent) StageChecks() []StageCheckProgress {
	if a == nil || !a.config.mayStage() {
		return nil
	}
	var out []StageCheckProgress
	seen := map[string]bool{}
	if a.jobs != nil {
		for _, j := range a.jobs.all() {
			j.mu.Lock()
			owned, id := j.stageOwned && !j.stageDelivered, j.callID
			j.mu.Unlock()
			if owned {
				out = append(out, StageCheckProgress{CallID: id, Command: verification.OriginalCommand(j.command), Started: j.started})
				seen[id] = true
			}
		}
	}
	// Snapshot pointers under the registry lock; BashCall owns its own process.
	a.inFlightBash.mu.Lock()
	calls := make(map[string]*bare.BashCall, len(a.inFlightBash.calls))
	for id, call := range a.inFlightBash.calls {
		calls[id] = call
	}
	a.inFlightBash.mu.Unlock()
	for id, call := range calls {
		command := verification.OriginalCommand(call.Command())
		if !seen[id] && verification.CheckCommand(command) {
			out = append(out, StageCheckProgress{CallID: id, Command: command, Started: time.Now().Add(-call.RunningFor())})
		}
	}
	return out
}

func (a *Agent) stageSteeringHeld() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, note := range a.steering {
		if note.steered || note.steer != nil {
			return true
		}
	}
	return false
}

// StageHasChecks is execution ownership, not a nullable journal exit.
func (a *Agent) StageHasChecks() bool {
	if a == nil || !a.config.mayStage() {
		return false
	}
	if a.jobs != nil && a.jobs.stageRunning() {
		return true
	}
	for _, call := range a.inFlightBash.snapshot() {
		if verification.CheckCommand(verification.OriginalCommand(call.Command())) {
			return true
		}
	}
	return false
}
