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
// prepareTitles initializes cleanup before live is published. Its reader starts
// only after publication, so replayed names see the held connection.
func (b *appBackend) prepareTitles(a *appAgent, live *appLive) func() {
	door, ok := live.conn.Conv.(interface {
		WatchTitle() (<-chan session.Event, func())
	})
	if !ok {
		return func() {}
	}
	lane, stop := door.WatchTitle()
	if lane == nil {
		if stop != nil {
			stop()
		}
		return func() {}
	}
	live.stopTitles, live.titlesDone = stop, make(chan struct{})
	return func() {
		guard.Go("app/titles", func() {
			defer close(live.titlesDone)
			for event := range lane {
				if event.Kind == session.EventTitleChanged {
					b.titleChanged(a, live, event.Text)
				}
			}
		})
	}
}

func (b *appBackend) titleChanged(a *appAgent, live *appLive, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
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
		// Snapshot and enqueue share the mutation lock: a later manual name
		// must never be overtaken by this generated snapshot.
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: a.view})
	})
}
