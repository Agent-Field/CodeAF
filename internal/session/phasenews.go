package session

import (
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── THE PHASE CLOCK, FORWARDED AND EXTENDED ─────────────────────────────────
//
// internal/provider knows what a REQUEST is doing — connecting, waiting for the
// first word, thinking, writing, paced, trying again, switching lanes. This
// package knows what a TURN is doing between requests — running a tool,
// checking an answer, tidying the transcript — and those waits are longer than
// any of the provider's: a `go test` runs for minutes, and the route judge that
// reads a finished answer has been measured at a quarter of an hour.
//
// Both halves are one story to the person watching, so they leave by one door.
// internal/tui3 imports this package and this package imports internal/provider,
// so the arrow points one way the whole distance (docs/ARCHITECTURE.md Decision
// 10): a surface registers [OnPhaseNews], this package forwards everything the
// transport posts and adds its own, and a build with no surface registers
// nothing and pays one nil check.
//
// THE VOCABULARY IS THE TRANSPORT'S, said once (internal/provider's phase.go).
// Two packages with two spellings of "thinking" is two surfaces that disagree
// the first time one of them is corrected.

// PhaseNews is one moment of one turn's life. It is [provider.PhaseNews] under
// this package's own name, so a surface imports one package rather than two.
type PhaseNews = provider.PhaseNews

// The phases a turn has that a request does not. They are spelled in
// internal/provider for the reason above — one vocabulary — and named here so a
// reader of this package can see the whole list in one place.
const (
	PhaseRunning  = provider.PhaseRunning
	PhaseChecking = provider.PhaseChecking
	PhaseTidying  = provider.PhaseTidying
)

var (
	phaseMu     sync.RWMutex
	phaseReader func(PhaseNews)
	// phasePrevious is the transport's reader as it was before this package
	// took it over, so that closing a surface really does put it back.
	phasePrevious  func(PhaseNews)
	phaseInstalled bool
)

// OnPhaseNews registers the reader every phase change is told to and hands back
// the one that was there. A nil function unregisters, and takes this package's
// forwarder off the transport with it.
func OnPhaseNews(fn func(PhaseNews)) (previous func(PhaseNews)) {
	phaseMu.Lock()
	previous, phaseReader = phaseReader, fn
	install := fn != nil && !phaseInstalled
	remove := fn == nil && phaseInstalled
	if install {
		phaseInstalled = true
	}
	if remove {
		phaseInstalled = false
	}
	phaseMu.Unlock()
	if install {
		phaseMu.Lock()
		phasePrevious = provider.OnPhase(forwardPhase)
		phaseMu.Unlock()
	}
	if remove {
		phaseMu.RLock()
		restore := phasePrevious
		phaseMu.RUnlock()
		provider.OnPhase(restore)
	}
	return previous
}

// forwardPhase carries a request's phase up to the surface unchanged. It is a
// named function rather than a literal so that what the transport is holding
// can be compared against what this package installed.
func forwardPhase(news PhaseNews) { postPhaseNews(news) }

// postPhaseNews tells whoever is listening. It never blocks on a slow reader
// and never panics on an absent one, for the reason [postLaneNews] does not: a
// measurement must not be able to break the turn it measured.
func postPhaseNews(news PhaseNews) {
	phaseMu.RLock()
	reader := phaseReader
	phaseMu.RUnlock()
	if reader == nil {
		return
	}
	if news.At.IsZero() {
		news.At = time.Now()
	}
	if news.Since.IsZero() {
		news.Since = news.At
	}
	reader(news)
}

// tellPhase is how this package says what a turn is doing between requests.
//
// It carries the agent's own model and role so that a surface can tell a
// conversation's wait from a task node's, and so that a side errand's phase
// never takes the clock away from the answer somebody is reading.
func (a *Agent) tellPhase(phase provider.Phase, detail string, since time.Time) {
	if a == nil {
		return
	}
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	postPhaseNews(PhaseNews{
		Phase:  phase,
		Since:  since,
		Detail: detail,
		Model:  model,
		Role:   a.laneRole(),
	})
}

// endPhase says the turn has stopped doing whatever it was doing, so a surface
// stops drawing a clock for work that is over. A stale phase left on the screen
// is exactly the defect the phase clock exists for.
func (a *Agent) endPhase() {
	if a == nil {
		return
	}
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	postPhaseNews(PhaseNews{Model: model, Role: a.laneRole()})
}

// laneRole is what this agent's own calls are for. A conversation is talk; a
// task node is a leaf, and which of the two leaf roles it is depends on the
// only thing that changes what a second is worth — whether anybody is here to
// read what it lands (see [Agent.turnLambda]).
func (a *Agent) laneRole() lane.Role {
	if a == nil {
		return lane.RoleUnknown
	}
	if !a.config.InTask {
		return lane.RoleTalk
	}
	if someoneIsWatching() {
		return lane.RoleLeafAttached
	}
	return lane.RoleLeafUnattended
}
