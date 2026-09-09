package session

import "fmt"

// requestIdentityAt reads the identity of the last human message, rather than
// the turn answering it. A job notification starts a turn without starting a
// new request. The admitting agent scopes the identity; text is never compared.
func requestIdentityAt(a *Agent) uint64 {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.personSeq
}

// backgroundWorkOwner distinguishes an unfinished goal from a new assignment.
// A turn that owes only background receipts reports them while its existing
// worker owns the work. A new human message or a task result creates a different
// obligation: new work, repair and integration remain possible. An unrelated
// older worker cannot claim the newer request, even when their wording matches.
func (a *Agent) backgroundWorkOwner() uint64 {
	if a.config.InTask {
		return 0
	}
	a.mu.Lock()
	if len(a.owedAsks) == 0 || a.personSeq == 0 || a.pendingSteerLocked() != nil {
		a.mu.Unlock()
		return 0
	}
	for _, owed := range a.owedAsks {
		if owed.from != owedByBackground {
			a.mu.Unlock()
			return 0
		}
	}
	request := a.personSeq
	a.mu.Unlock()
	graph := a.tasker()
	if graph == nil {
		return 0
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	moving := make(map[uint64]bool)
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node != nil && node.parent == 0 && node.admitBy == a &&
			node.admitRequest == request && graph.movingLocked(id, moving) {
			return id
		}
	}
	return 0
}

// The same explanation answers both a proposed duplicate and a checkpoint.
// It promises a result from the existing owner, never completion of the goal.
func backgroundOwnerNote(id uint64) string {
	return fmt.Sprintf("task %d is still working on this request · keeping its work there and waiting for its result", id)
}
