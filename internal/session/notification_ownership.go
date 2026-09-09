package session

import (
	"fmt"
	"strings"
)

// backgroundWorkOwner distinguishes an unfinished goal from a new assignment.
// Each background receipt keeps the human request that started its operation.
// A report waits only when EVERY receipt belongs to work this session still has
// out. An unknown origin, main-owned work, a new human message or a task result
// keeps ordinary continuation available. No words or current wake epochs stand
// in for the identity of the work.
func (a *Agent) backgroundWorkOwner() uint64 {
	if a.config.InTask {
		return 0
	}
	a.mu.Lock()
	if len(a.owedAsks) == 0 || a.pendingSteerLocked() != nil {
		a.mu.Unlock()
		return 0
	}
	requests := make(map[uint64]bool)
	for _, owed := range a.owedAsks {
		if owed.from != owedByBackground || owed.request == 0 {
			a.mu.Unlock()
			return 0
		}
		requests[owed.request] = false
	}
	a.mu.Unlock()
	graph := a.tasker()
	if graph == nil {
		return 0
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	moving := make(map[uint64]bool)
	var first uint64
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node == nil || node.parent != 0 || node.admitBy != a {
			continue
		}
		if _, needed := requests[node.admitRequest]; needed && graph.movingLocked(id, moving) {
			requests[node.admitRequest] = true
			if first == 0 {
				first = id
			}
		}
	}
	for _, owned := range requests {
		if !owned {
			return 0
		}
	}
	return first
}

// The same explanation answers both a proposed duplicate and a checkpoint.
// It promises a result from the existing owner, never completion of the goal.
func backgroundOwnerNote(id uint64) string {
	return fmt.Sprintf("task %d is still working on this request · keeping its work there and waiting for its result", id)
}

// A checkpoint may end the turn before the model has summarized its receipts.
// Carry their actual outcomes into the answer so parking never drops the report
// owed for a main-session job that the worker cannot see.
func (a *Agent) backgroundOwnerReport(id uint64) string {
	a.mu.Lock()
	var outcomes []string
	for _, owed := range a.owedAsks {
		if owed.from == owedByBackground && strings.TrimSpace(owed.outcome) != "" {
			outcomes = append(outcomes, owed.outcome)
		}
	}
	a.mu.Unlock()
	return strings.Join(append(outcomes, backgroundOwnerNote(id)), "\n\n")
}
