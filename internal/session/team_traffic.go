package session

import (
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

var teamTraffic teamTrafficWatch

// teamTrafficWatch keeps one clock for membership discovery and team traffic.
// A slow conversation runs independently and never overlaps its own last check.
type teamTrafficWatch struct {
	mu  sync.Mutex
	run *teamTrafficRun
}

type teamTrafficRun struct {
	stop    chan struct{}
	clients map[*Agent]*teamTrafficClient
}

type teamTrafficClient struct {
	profile string
	at      fileStamp
	active  bool
	busy    bool
}

func (w *teamTrafficWatch) add(a *Agent, profile string) {
	a.team.mu.Lock()
	c := &teamTrafficClient{profile: profile, at: a.team.teamsAt, active: len(a.team.roles) > 0 || len(a.team.wraps) > 0}
	a.team.mu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.run == nil {
		w.run = &teamTrafficRun{stop: make(chan struct{}), clients: map[*Agent]*teamTrafficClient{}}
		run := w.run
		guard.Go("team traffic watch", func() { w.loop(run) })
	}
	w.run.clients[a] = c
}

func (w *teamTrafficWatch) remove(a *Agent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.run == nil {
		return
	}
	delete(w.run.clients, a)
	if len(w.run.clients) == 0 {
		close(w.run.stop)
		w.run = nil
	}
}

func (w *teamTrafficWatch) loop(run *teamTrafficRun) {
	ticker := time.NewTicker(teamWatchInterval())
	defer ticker.Stop()
	for {
		select {
		case <-run.stop:
			return
		case now := <-ticker.C:
			w.tick(run, now)
		}
	}
}

func (w *teamTrafficWatch) tick(run *teamTrafficRun, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.run != run {
		return
	}
	for a, c := range run.clients {
		if c.busy || (!c.active && c.at == sharedTeamsStamp(c.profile)) {
			continue
		}
		c.busy = true
		guard.Go("team traffic wake", func() {
			defer func() {
				w.mu.Lock()
				c.busy = false
				w.mu.Unlock()
			}()
			if !a.teamWatchTick(c.profile, now) {
				w.remove(a)
				return
			}
			a.team.mu.Lock()
			at, active := a.team.teamsAt, len(a.team.roles) > 0 || len(a.team.wraps) > 0
			a.team.mu.Unlock()
			w.mu.Lock()
			c.at, c.active = at, active
			w.mu.Unlock()
		})
	}
}
