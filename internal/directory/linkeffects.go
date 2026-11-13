package directory

// Bind reserves a temporary request for one cluster before it can enter that
// cluster's committed outbox. Legacy decisions cannot write a bound request.
// A failed admission leaves the reservation until expiry; it never permits two
// identities to acknowledge different decisions for the same public code.
func (l *Links) Bind(code, cluster string) (Request, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(l.now())
	e := l.reqs[NormalizeCode(code)]
	if e == nil {
		return Request{}, ErrRequestGone
	}
	if cluster == "" || e.cluster != "" && e.cluster != cluster || e.cluster == "" && e.rec.State != RequestPending {
		return Request{}, ErrAlreadyDecided
	}
	e.cluster = cluster
	return e.rec, nil
}

// Deliver is an internal outbox destination, not a public pairing decision.
// The caller has already read the exact committed decision through a barrier.
func (l *Links) Deliver(code, cluster, id string, d Decision) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(l.now())
	e := l.reqs[NormalizeCode(code)]
	if e == nil {
		return ErrRequestGone
	}
	if id == "" || cluster == "" || e.cluster != cluster || d.Device != e.rec.Device {
		return ErrAlreadyDecided
	}
	if e.delivery != "" {
		if e.delivery == id && (d.State == e.rec.State && d.sameBody(e.rec)) {
			return nil
		}
		return ErrAlreadyDecided
	}
	if d.State != RequestApproved && d.State != RequestDenied || d.State == RequestApproved && (d.Device != e.rec.Device || d.Grant == "") {
		return ErrBadRequest
	}
	if e.rec.State != RequestPending {
		return ErrAlreadyDecided
	}
	e.rec, e.until = Decided(e.rec, d), l.now()+DecidedKeep.Milliseconds()
	e.delivery = id
	close(e.done)
	return nil
}
