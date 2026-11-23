package main

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/offpath"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appSpendRestates coalesces news per conversation. Its lock protects only
// handoffs, never disk reads, the backend lock or saying an agent to the window.
type appSpendRestates struct {
	mu      sync.Mutex
	of      map[string]*offpath.Write
	stopped bool
}

// watchAgentSpend follows saved conversation totals separately from the
// machine ledger: a late receipt can save spend after its turn has ended.
func (b *appBackend) watchAgentSpend() (stop func()) {
	unwatch := session.OnSpendSaved(b.agentSpendSaved)
	return func() {
		unwatch()
		for _, restate := range b.spendRestates.stop() {
			restate.Settle()
		}
	}
}

// stop refuses later news and answers the restates already handed off, which
// the caller settles outside the lock.
func (s *appSpendRestates) stop() []*offpath.Write {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	return s.pendingLocked()
}

// settle joins the restates already handed off, outside the lock.
func (s *appSpendRestates) settle() {
	for _, restate := range s.pending() {
		restate.Settle()
	}
}

func (s *appSpendRestates) pending() []*offpath.Write {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingLocked()
}

func (s *appSpendRestates) pendingLocked() []*offpath.Write {
	pending := make([]*offpath.Write, 0, len(s.of))
	for _, restate := range s.of {
		pending = append(pending, restate)
	}
	return pending
}

// agentSpendSaved only hands off identity. Even delayed news asks what is on
// disk now; neither its figure nor a producer's copied view is a spend cache.
func (b *appBackend) agentSpendSaved(saved session.SpendSaved) {
	dir := strings.TrimSpace(saved.Dir)
	if dir == "" {
		return
	}
	dir = filepath.Clean(dir)
	s := &b.spendRestates
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	if s.of == nil {
		s.of = map[string]*offpath.Write{}
	}
	restate := s.of[dir]
	if restate == nil {
		restate = offpath.Deferred(func() { b.restateSpend(dir) })
		s.of[dir] = restate
	}
	restate.Owe()
}

// restateSpend mirrors restateUnread: compare with what was last said under
// the saying lock, then use the ordinary door, which dresses afresh at delivery.
func (b *appBackend) restateSpend(dir string) {
	id := filepath.Base(dir)
	var a *appAgent
	stale := false
	b.sayingUnread(func() {
		var said float64
		b.locked(func() {
			if b.closing {
				return
			}
			if have := b.agents[id]; have != nil && have.transcript != "" && session.DirOf(have.transcript) == dir {
				a, said = have, have.saidSpend
			}
		})
		if a != nil {
			spent, err := session.SavedSpend(dir)
			stale = err == nil && spent != said
		}
	})
	if stale {
		b.locked(func() { stale = !b.closing })
		if stale {
			b.sayListed(a, id)
		}
	}
}

// savedSpendOf reads only the saved figure, without LoadMeta's journal and
// workspace overlays. It never takes unreadSay or records the figure as said,
// so an already-opening answer need not wait for the window's event writer.
func (b *appBackend) savedSpendOf(id string) float64 {
	var transcript string
	b.locked(func() {
		if a := b.agents[id]; a != nil {
			transcript = a.transcript
		}
	})
	if transcript == "" {
		return 0
	}
	spent, _ := session.SavedSpend(session.DirOf(transcript))
	return spent
}

// dressSpendLocked fills the saved figure and remembers it as said. Callers
// hold unreadSay through remembering and saying it.
func (b *appBackend) dressSpendLocked(view *appwire.Agent) {
	view.SpentUSD = b.savedSpendOf(view.ID)
	b.locked(func() {
		if a := b.agents[view.ID]; a != nil {
			a.saidSpend = view.SpentUSD
		}
	})
}
