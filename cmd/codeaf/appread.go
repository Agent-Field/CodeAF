package main

// readAgent pins a conversation for the whole caller's read, including its
// disk reads and the final owner overlay. Opening and pinning share admission's
// gate, so release cannot fit between them.
func (b *appBackend) readAgent(id string, owner bool) (*appAgent, func(), error) {
	a, err := b.find(id)
	if err != nil {
		return nil, nil, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if b.crewKeeps(id) && (owner || !b.memberRests(a)) {
		if _, _, err := b.heldLiveLocked(a); err != nil {
			return nil, nil, err
		}
	}
	return a, b.pinReader(a), nil
}

// pinReader is called under the member gate. The count and its completion
// signal are under b.mu so returning a reader never needs the admission gate.
func (b *appBackend) pinReader(a *appAgent) func() {
	b.locked(func() {
		if a.readers == 0 {
			a.readersDone = make(chan struct{})
		}
		a.readers++
	})
	return func() {
		rest := false
		b.locked(func() {
			a.readers--
			if a.readers == 0 {
				close(a.readersDone)
				rest = a.restRead
			}
		})
		if rest {
			b.letRestingGo(a)
		}
	}
}

// borrowLive keeps the conversation alive through a reader's last use.
func (b *appBackend) borrowLive(id string) (*appAgent, *appLive, func(), error) {
	a, err := b.find(id)
	if err != nil {
		return nil, nil, nil, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		return nil, nil, nil, err
	}
	return a, live, b.pinReader(a), nil
}

// admitLive holds the same gate as idle release until the engine has admitted
// new work. A background task need not set the foreground live.running flag.
func (b *appBackend) admitLive(id string) (*appAgent, *appLive, func(), error) {
	a, err := b.find(id)
	if err != nil {
		return nil, nil, nil, err
	}
	a.gate.Lock()
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		a.gate.Unlock()
		return nil, nil, nil, err
	}
	return a, live, a.gate.Unlock, nil
}

// lockRelease waits for readers outside the admission gate, then checks again
// under it. A reader may itself need that gate before returning its reference.
func (b *appBackend) lockRelease(a *appAgent) {
	for {
		a.gate.Lock()
		var done <-chan struct{}
		b.locked(func() {
			if a.readers > 0 {
				done = a.readersDone
			}
		})
		if done == nil {
			return
		}
		a.gate.Unlock()
		if b.waitReaders != nil {
			b.waitReaders(done)
		} else {
			<-done
		}
	}
}

// borrowExisting pins only an already-held conversation; suggestions and naming
// reads must never open an idle owner as a side effect.
func (b *appBackend) borrowExisting(id string) (*appAgent, *appLive, func(), error) {
	a, err := b.find(id)
	if err != nil {
		return nil, nil, nil, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	return a, b.liveOf(a), b.pinReader(a), nil
}
