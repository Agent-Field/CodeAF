package session

// backgroundResult is an outcome and the request that started its work. The
// request is captured before a job begins, never inferred from the latest words
// when it finishes. Zero means that the runtime cannot establish an origin.
type backgroundResult struct {
	text    string
	request uint64
}

// requestForWork keeps a follow-up on the request whose outcome caused it. A
// person's words establish a new origin; a result inherits its producer's. Mixed
// or unknown origins claim nothing rather than borrowing the latest human ask.
func (a *Agent) requestForWork() uint64 {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	if !a.running || a.pendingSteerLocked() != nil {
		a.mu.Unlock()
		return 0
	}
	owed := append([]owedAsk(nil), a.owedAsks...)
	arrived := append([]uint64(nil), a.turnResults...)
	person := a.personSeq
	a.mu.Unlock()
	for _, ask := range owed {
		if ask.from == owedByPerson {
			return person
		}
	}
	var request uint64
	for _, ask := range owed {
		origin := ask.request
		if ask.from == owedByResult {
			origin = a.resultRequest(ask.task)
		}
		if origin == 0 || (request != 0 && request != origin) {
			return 0
		}
		request = origin
	}
	// Equal result obligations may share one displayed ask, but every producer
	// must agree before their follow-up can inherit a single request.
	for _, id := range arrived {
		origin := a.resultRequest(id)
		if origin == 0 || (request != 0 && request != origin) {
			return 0
		}
		request = origin
	}
	return request
}

// resultRequest follows a task result to its admission in this conversation.
// A restored or foreign node has no proven origin in this session's sequence.
func (a *Agent) resultRequest(id uint64) uint64 {
	graph := a.tasker()
	if graph == nil {
		return 0
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	if node := graph.nodes[id]; node != nil && node.admitBy == a {
		return node.admitRequest
	}
	return 0
}
