package main

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const planCountThrottle = 2 * time.Second

// headlessPlanProgress writes progress apart from command output. In
// particular, JSON written by -o or --json never acquires status lines.
func headlessPlanProgress(writer io.Writer) plan.Progress {
	var mutex sync.Mutex
	return func(update plan.ProgressUpdate) {
		mutex.Lock()
		defer mutex.Unlock()
		fmt.Fprintln(writer, planProgressLine(update))
	}
}

func planProgressLine(update plan.ProgressUpdate) string {
	line := update.Phase
	if update.Total > 0 {
		line += fmt.Sprintf(" · %d of %d", update.Done, update.Total)
	}
	return line
}

type planProgressPoster struct {
	history  *store.Store
	anchor   resident.PlanAnchor
	interval time.Duration
	now      func() time.Time

	mutex   sync.Mutex
	last    map[string]time.Time
	pending map[string]plan.ProgressUpdate
	timers  map[string]*time.Timer
	// posted remembers the last line each phase produced: a planning pass that
	// re-announces the same state (retries, re-polls) must not repeat itself
	// into the thread.
	posted map[string]string
}

func chatPlanProgress(history *store.Store, anchor resident.PlanAnchor) plan.Progress {
	if history == nil || anchor.NodeID == "" {
		return nil
	}
	poster := &planProgressPoster{
		history: history, anchor: anchor, interval: planCountThrottle, now: time.Now,
		last: map[string]time.Time{}, pending: map[string]plan.ProgressUpdate{}, timers: map[string]*time.Timer{},
		posted: map[string]string{},
	}
	return poster.report
}

func (p *planProgressPoster) report(update plan.ProgressUpdate) {
	phase := update.Phase
	count := update.Total > 0

	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.ensureMaps()
	// Real generated content is never coalesced away: the card needs each title
	// in order to materialize its honest three-line table of contents.
	if !count {
		p.post(update)
		return
	}
	if update.Latest != "" {
		p.stopTimer(phase)
		delete(p.pending, phase)
		p.last[phase] = p.now()
		p.post(update)
		return
	}

	now := p.now()
	last := p.last[phase]
	final := update.Done == update.Total
	if final || last.IsZero() || now.Sub(last) >= p.interval {
		p.stopTimer(phase)
		delete(p.pending, phase)
		p.last[phase] = now
		p.post(update)
		return
	}

	p.pending[phase] = update
	if p.timers[phase] == nil {
		wait := p.interval - now.Sub(last)
		p.timers[phase] = time.AfterFunc(wait, func() {
			defer guard.Recover("chat/plan-progress flush")
			p.flushCount(phase)
		})
	}
}

func (p *planProgressPoster) flushCount(phase string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.ensureMaps()
	update, ok := p.pending[phase]
	if !ok {
		p.stopTimer(phase)
		return
	}
	delete(p.pending, phase)
	delete(p.timers, phase)
	p.last[phase] = p.now()
	p.post(update)
}

// ensureMaps makes the zero poster work. Every field here is bookkeeping the
// constructor fills, and a caller that builds the struct directly — as the
// tests do — would otherwise write to a nil map and take the process down.
// Callers hold the mutex.
func (p *planProgressPoster) ensureMaps() {
	if p.last == nil {
		p.last = map[string]time.Time{}
	}
	if p.pending == nil {
		p.pending = map[string]plan.ProgressUpdate{}
	}
	if p.timers == nil {
		p.timers = map[string]*time.Timer{}
	}
	if p.posted == nil {
		p.posted = map[string]string{}
	}
}

func (p *planProgressPoster) stopTimer(stage string) {
	if timer := p.timers[stage]; timer != nil {
		timer.Stop()
		delete(p.timers, stage)
	}
}

func (p *planProgressPoster) post(update plan.ProgressUpdate) {
	line := planProgressLine(update)
	p.ensureMaps()
	if update.Latest == "" && p.posted[update.Phase] == line {
		return
	}
	p.posted[update.Phase] = line
	_, _ = p.history.PostMessage(store.Message{
		SessionID:  p.anchor.SessionID,
		Role:       store.RoleSystem,
		Body:       planProgressLine(update),
		NodeID:     p.anchor.NodeID,
		CommandSeq: p.anchor.CommandSeq,
		Progress: &store.MessageProgress{
			Phase: update.Phase, Done: update.Done, Total: update.Total, Latest: update.Latest,
		},
	})
}
