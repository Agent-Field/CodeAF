package main

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const planCountThrottle = 2 * time.Second

// headlessPlanProgress writes progress apart from command output. In
// particular, JSON written by -o or --json never acquires status lines.
func headlessPlanProgress(writer io.Writer) plan.Progress {
	var mutex sync.Mutex
	return func(stage, detail string) {
		mutex.Lock()
		defer mutex.Unlock()
		fmt.Fprintln(writer, planProgressLine(stage, detail))
	}
}

func planProgressLine(stage, detail string) string {
	if _, _, ok := leafCount(stage, detail); ok || stage == "sizing" {
		return stage + " " + detail
	}
	return stage + ": " + detail
}

type planProgressPoster struct {
	history  *store.Store
	anchor   resident.PlanAnchor
	interval time.Duration
	now      func() time.Time

	mutex   sync.Mutex
	last    map[string]time.Time
	pending map[string]string
	timers  map[string]*time.Timer
}

func chatPlanProgress(history *store.Store, anchor resident.PlanAnchor) plan.Progress {
	if history == nil || anchor.NodeID == "" {
		return nil
	}
	poster := &planProgressPoster{
		history: history, anchor: anchor, interval: planCountThrottle, now: time.Now,
		last: map[string]time.Time{}, pending: map[string]string{}, timers: map[string]*time.Timer{},
	}
	return poster.report
}

func (p *planProgressPoster) report(stage, detail string) {
	line := planProgressLine(stage, detail)
	done, total, count := leafCount(stage, detail)

	p.mutex.Lock()
	defer p.mutex.Unlock()
	if !count {
		p.post(line)
		return
	}

	now := p.now()
	last := p.last[stage]
	final := done == total
	if final || last.IsZero() || now.Sub(last) >= p.interval {
		p.stopTimer(stage)
		delete(p.pending, stage)
		p.last[stage] = now
		p.post(line)
		return
	}

	p.pending[stage] = line
	if p.timers[stage] == nil {
		wait := p.interval - now.Sub(last)
		p.timers[stage] = time.AfterFunc(wait, func() { p.flushCount(stage) })
	}
}

func (p *planProgressPoster) flushCount(stage string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	line := p.pending[stage]
	if line == "" {
		p.stopTimer(stage)
		return
	}
	delete(p.pending, stage)
	delete(p.timers, stage)
	p.last[stage] = p.now()
	p.post(line)
}

func (p *planProgressPoster) stopTimer(stage string) {
	if timer := p.timers[stage]; timer != nil {
		timer.Stop()
		delete(p.timers, stage)
	}
}

func (p *planProgressPoster) post(body string) {
	_, _ = p.history.PostMessage(store.Message{
		SessionID:  p.anchor.SessionID,
		Role:       store.RoleSystem,
		Body:       body,
		NodeID:     p.anchor.NodeID,
		CommandSeq: p.anchor.CommandSeq,
	})
}

func leafCount(stage, detail string) (int, int, bool) {
	if stage != "briefs" && stage != "contracts" {
		return 0, 0, false
	}
	var done, total int
	if _, err := fmt.Sscanf(detail, "%d/%d", &done, &total); err != nil ||
		done < 0 || total < 0 || done > total {
		return 0, 0, false
	}
	return done, total, true
}
