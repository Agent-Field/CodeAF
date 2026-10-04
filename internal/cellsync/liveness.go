package cellsync

// Liveness is a watch socket that can vouch for the leases this process holds
// (STAGE-1-CONTRACTS.md §21.11). While it does, the relay counts the socket's
// pings as the holder's proof of life, so the Batcher need not heartbeat.
type Liveness interface {
	// Keep names the lease of cell at fence on the socket and answers how to
	// read whether it is vouched for. The socket dials again to name it.
	Keep(cell string, fence uint64) Keeping
}

// Keeping is one lease as the socket names it.
type Keeping interface {
	// Healthy is true only while the relay is certain to be vouching: the
	// socket is open, the relay said it vouches, the socket names this lease,
	// and it showed a sign of life lately. Anything less, and the holder beats.
	Healthy() bool
	// Changes signals, without blocking, that Healthy may have changed.
	Changes() <-chan struct{}
	// Release stops naming the lease.
	Release()
}

// holding is the Batcher's current Keeping, re-made when the lease it names
// changes. Its zero value holds nothing and is never healthy.
type holding struct {
	keeping Keeping
	cell    string
	fence   uint64
	vouched bool // the socket was healthy when last looked at
}

// follow makes the socket name the lease the Batcher holds now: the cell and
// fence of the driving state, and nothing while the lease is lost or not yet
// taken, because there is nothing to vouch for.
func (h *holding) follow(l Liveness, view func() (Driving, uint32, bool)) {
	if l == nil {
		return
	}
	d, _, stale := view()
	cell, fence := d.ID(), d.Fence
	if stale || fence == 0 {
		h.release()
		return
	}
	if h.keeping != nil && h.cell == cell && h.fence == fence {
		return
	}
	h.release()
	h.keeping, h.cell, h.fence, h.vouched = l.Keep(cell, fence), cell, fence, false
}

// covers says whether the lease needs no heartbeat now: the socket is healthy
// and the directory already knows how many turns are pending.
func (h *holding) covers(view func() (Driving, uint32, bool), told func(uint32) bool) bool {
	h.vouched = h.keeping != nil && h.keeping.Healthy()
	if !h.vouched {
		return false
	}
	_, pending, _ := view()
	return told(pending)
}

// lapsed says whether the socket vouched when last looked at and no longer
// does. Only that is news to the lease: a socket that was never vouching and
// redials has changed nothing, and the tick already beats for it.
func (h *holding) lapsed() bool {
	was := h.vouched
	h.vouched = h.keeping != nil && h.keeping.Healthy()
	return was && !h.vouched
}

// changes is the channel the socket signals on, or nil, which never fires.
func (h *holding) changes() <-chan struct{} {
	if h.keeping == nil {
		return nil
	}
	return h.keeping.Changes()
}

func (h *holding) release() {
	if h.keeping != nil {
		h.keeping.Release()
		h.keeping = nil
	}
}
