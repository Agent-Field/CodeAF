package main

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── a wait before the step is asked again ───────────────────────────────────
//
// A provider that refuses a request (busy, overloaded, a rate limit) is asked
// again after a pause, and the engine says that pause as a phase with the
// moment the request goes out again on it. The app reads the same phases the
// terminal draws its status line from, so the countdown a person sees in the
// app is the wait the engine is really sitting out.
//
// A REFUSED CONNECTION IS NOT RETRIED, IN THE APP OR IN THE TERMINAL. The engine
// reads it as no network and waits for the network to come back for up to two
// minutes, then ends the reply with "connection is still unavailable"; that
// wait is said here as a retry with the reason offline.

// The reasons a retry says beside its countdown.
const (
	appRetryPaced   = "paced"
	appRetryOffline = "offline"
)

// appRetrySlack is how early before a wait's end news of a new wait of the
// same kind may arrive and still be a new one.
const appRetrySlack = 500 * time.Millisecond

// appPhaseReader is what the app put in front of the engine's phase news, kept
// so closing puts the previous reader back.
type appPhaseReader struct {
	previous atomic.Pointer[func(session.PhaseNews)]
}

// listenPhases starts reading the engine's phase news, and answers the function
// that stops.
func (b *appBackend) listenPhases() func() {
	reader := &appPhaseReader{}
	previous := session.OnPhaseNews(func(news session.PhaseNews) {
		if next := reader.previous.Load(); next != nil && *next != nil {
			(*next)(news)
		}
		b.phaseNews(news)
	})
	reader.previous.Store(&previous)
	return func() { session.OnPhaseNews(previous) }
}

// phaseNews says a conversation's wait before its reply is asked again.
func (b *appBackend) phaseNews(news session.PhaseNews) {
	// Only the reply a person is reading is said: a task's own request and the
	// errands beside a reply (its naming, its memory) have their own pages.
	if news.Subject != "" || news.Role != lane.RoleTalk {
		return
	}
	reason := ""
	switch news.Phase {
	case provider.PhaseRetrying:
	case provider.PhasePaced:
		reason = appRetryPaced
	case provider.PhaseConnectionLost:
		reason = appRetryOffline
	default:
		return
	}
	if news.Deadline.IsZero() {
		return
	}
	var live *appLive
	b.locked(func() {
		if a := b.agents[news.Session]; a != nil {
			live = a.live
		}
	})
	if live == nil {
		return
	}
	// The engine says a held stage again every few seconds, and a stage that
	// crossed from a host has its moments measured again on arrival; one wait
	// is said once, so news of the same kind before the last wait's end is the
	// same wait.
	at := news.At
	if at.IsZero() {
		at = time.Now()
	}
	var retry appwire.TurnRetrying
	said := false
	b.locked(func() {
		if !live.running || live.closed {
			return
		}
		if live.retryPhase == news.Phase && at.Before(live.retryUntil.Add(-appRetrySlack)) {
			return
		}
		live.retryPhase, live.retryUntil = news.Phase, news.Deadline
		live.retries++
		live.retrySaid = true
		retry = appwire.TurnRetrying{Agent: news.Session, Turn: live.turn, Attempt: live.retries, Reason: reason}
		said = true
	})
	if !said {
		return
	}
	in := max(0, int(math.Ceil(news.Deadline.Sub(at).Seconds())))
	retry.InS = &in
	live.box.Event(appwire.EventTurnRetrying, retry)
}
