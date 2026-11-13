package main

import "sync"

// readAgent pins a conversation for the whole caller's read, including its
// disk reads and the final owner overlay. An already-held conversation is
// pinned under b.mu without waiting on the owner gate; opening and pinning a
// busy crew member share admission's gate so release cannot fit between them.
func (b *appBackend) readAgent(id string, owner bool) (*appAgent, func(), error) {
	a, err := b.find(id)
	if err != nil {
		return nil, nil, err
	}
	var opening bool
	var endRead func()
	b.locked(func() {
		opening = a.opening
		if a.live != nil && !a.releasing {
			endRead = b.pinReaderLocked(a)
		}
	})
	if endRead != nil {
		return a, endRead, nil
	}
	if opening || b.isClosing() || !b.crewKeeps(id) || b.tidied(id) || (!owner && b.memberRests(a)) {
		return a, func() {}, nil
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if b.tidied(id) || b.isClosing() {
		return a, func() {}, nil
	}
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
	var endRead func()
	b.locked(func() {
		endRead = b.pinReaderLocked(a)
	})
	return endRead
}

func (b *appBackend) pinReaderLocked(a *appAgent) func() {
	if a.readers == 0 {
		a.readersDone = make(chan struct{})
	}
	a.readers++
	var once sync.Once
	return func() {
		once.Do(func() {
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
		})
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

// borrowKeptLive is borrowLive for background paths that must not reopen a
// conversation the person has closed.
func (b *appBackend) borrowKeptLive(id string) (*appAgent, *appLive, func(), error) {
	a, err := b.find(id)
	if err != nil {
		return nil, nil, nil, err
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	if b.tidied(id) {
		return nil, nil, nil, errAppKeptClosed
	}
	_, live, err := b.heldLiveLocked(a)
	if err != nil {
		return nil, nil, nil, err
	}
	return a, live, b.pinReader(a), nil
}

// pinReaderGated pins a reader under the member gate.
func (b *appBackend) pinReaderGated(a *appAgent) func() {
	a.gate.Lock()
	defer a.gate.Unlock()
	return b.pinReader(a)
}

// lockRelease waits for readers outside the admission gate, then checks again
// under it. A reader may itself need that gate before returning its reference.
// Explicit release and quit remain bounded by appReleaseWait so a wedged reader
// cannot stall closing. Callers hold a.gate.
func (b *appBackend) lockRelease(a *appAgent) {
	var deadline chan struct{}
	var stop func()
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	for {
		var done <-chan struct{}
		b.locked(func() {
			if a.readers > 0 {
				done = a.readersDone
			} else {
				a.releasing = true
			}
		})
		if done == nil {
			return
		}
		if b.waitReaders == nil && deadline == nil {
			deadline = make(chan struct{})
			stop = b.after(appReleaseWait, func() { close(deadline) })
		}
		if b.waitReaderTurnUnlocked(a, done, deadline) {
			b.locked(func() { a.releasing = true })
			return
		}
	}
}

func (b *appBackend) waitReaderTurnUnlocked(a *appAgent, done, deadline <-chan struct{}) bool {
	a.gate.Unlock()
	defer a.gate.Lock()
	if b.waitReaders != nil {
		b.waitReaders(done)
		return false
	}
	select {
	case <-done:
		return false
	case <-deadline:
		return true
	}
}

// borrowExisting pins only an already-held conversation; suggestions and naming
// reads must never open an idle owner as a side effect.
func (b *appBackend) borrowExisting(id string) (*appAgent, *appLive, func(), error) {
	a, err := b.find(id)
	if err != nil {
		return nil, nil, nil, err
	}
	var live *appLive
	var endRead func()
	b.locked(func() {
		if a.live != nil && !a.releasing {
			live = a.live
			endRead = b.pinReaderLocked(a)
		}
	})
	if endRead == nil {
		endRead = func() {}
	}
	return a, live, endRead, nil
}
