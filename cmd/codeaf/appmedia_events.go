package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"strings"
)

const appMediaRecentMax = 128

// Turn streams and standing watchers can carry the same append. This is only a
// bounded recent-event filter, never an all-history source cache or authority.
func (b *appBackend) mediaChanged(agent string, live *appLive, change session.MediaChange) {
	if live == nil || change.ID == "" || len(change.ID) > 128 {
		return
	}
	event := appwire.MediaSourceChanged{Agent: agent, Source: strings.Clone(change.ID)}
	if len(change.Run) <= 1024 {
		event.Run = strings.Clone(change.Run)
	}
	if len(change.Call) <= 1024 {
		event.Call = strings.Clone(change.Call)
	}
	fresh := false
	b.locked(func() {
		if live.mediaSeen == nil {
			live.mediaSeen = map[string]bool{}
		}
		if live.mediaSeen[change.ID] {
			return
		}
		live.mediaSeen[event.Source] = true
		live.mediaRecent = append(live.mediaRecent, event.Source)
		fresh = true
		if len(live.mediaRecent) > appMediaRecentMax {
			delete(live.mediaSeen, live.mediaRecent[0])
			copy(live.mediaRecent, live.mediaRecent[1:])
			live.mediaRecent = live.mediaRecent[:appMediaRecentMax]
		}
	})
	if fresh {
		live.box.Event(appwire.EventMediaChanged, event)
	}
}
