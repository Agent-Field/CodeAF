package tui3

import "sync"

// serviceLands is the desk between a fetch goroutine and the update loop: the
// pairs a warm or a ctrl+r walk landed since the loop last read. The goroutine
// that stocked a compartment writes here and rings [app.landedBell]; the Update
// that takes the ring reads and clears the desk ON the loop, drops the memo
// under each pair, and restocks an open picker — so a group fills WITHOUT a
// reopen and never off the loop (issue #1508).
type serviceLands struct {
	mu     sync.Mutex
	closed bool
	pairs  map[[2]string]bool
}

// put records one landed pair. Safe from any goroutine.
func (l *serviceLands) put(source, address string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	if l.pairs == nil {
		l.pairs = make(map[[2]string]bool)
	}
	l.pairs[[2]string{source, address}] = true
	return true
}

// take reads and clears the desk. Called on the loop only.
func (l *serviceLands) take() [][2]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([][2]string, 0, len(l.pairs))
	for pair := range l.pairs {
		out = append(out, pair)
	}
	l.pairs = map[[2]string]bool{}
	return out
}

// serviceModelsLandedMsg is the message the door carries. Nothing is read out
// of it: the pairs are on the desk ([serviceLands]) by the time it arrives,
// and it exists only to bring the loop back around to read them.
type serviceModelsLandedMsg struct{}

// listenForServiceModels joins the process fan-out before Init can warm a
// provider. A callback already in flight after unsubscribe is harmless.
func listenForServiceModels(a *app, subscribe func(func(string, string)) func()) func() {
	if subscribe == nil {
		return func() {}
	}
	desk := &serviceLands{}
	bell := newDoorbell(serviceModelsLandedMsg{})
	a.serviceLands, a.landedBell = desk, bell
	unsubscribe := subscribe(func(source, address string) {
		if desk.put(source, address) {
			bell.ring()
		}
	})
	return func() {
		desk.mu.Lock()
		desk.closed = true
		desk.pairs = nil
		desk.mu.Unlock()
		if unsubscribe != nil {
			unsubscribe()
		}
		bell.close()
	}
}
