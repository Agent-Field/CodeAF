package main

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A name belongs to the conversation, not the turn: a short answer often ends
// before its optional naming request. Hold the existing standing lane for the
// same lifetime as the held connection, and leave it before closing the outbox.
func (b *appBackend) watchTitles(a *appAgent, live *appLive) {
	door, ok := live.conn.Conv.(interface {
		WatchTitle() (<-chan session.Event, func())
	})
	if !ok {
		return
	}
	lane, stop := door.WatchTitle()
	if lane == nil {
		if stop != nil {
			stop()
		}
		return
	}
	live.stopTitles, live.titlesDone = stop, make(chan struct{})
	guard.Go("app/titles", func() {
		defer close(live.titlesDone)
		for event := range lane {
			if event.Kind == session.EventTitleChanged {
				b.titleChanged(a, live, event.Text)
			}
		}
	})
}

func (b *appBackend) titleChanged(a *appAgent, live *appLive, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	var view appwire.Agent
	changed := false
	b.locked(func() {
		if a.live != live || live.closed {
			return
		}
		// The remote replica takes versioned facts before delivering a title.
		// Read its latest name so an older queued notification cannot restore it.
		if current := strings.TrimSpace(live.conn.Conv.Title()); current != "" {
			title = current
		}
		if a.view.Title == title {
			return
		}
		a.view.Title = title
		view = a.view
		changed = true
	})
	if changed {
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}
