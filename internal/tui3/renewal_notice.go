package tui3

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// renewalReader reports the first failed renewal, its period, and whether this
// conversation still holds the chat. A successful renewal clears the time.
// The reading is local and never waits for the network.
type renewalReader interface {
	RenewalStatus() (time.Time, time.Duration, bool)
}

func (a *app) takeRenewalNotice() {
	source, ok := a.agent.(renewalReader)
	if !ok {
		return
	}
	since, period, held := source.RenewalStatus()
	if !held {
		delete(a.syncOutages, a.file)
		return
	}
	if since.IsZero() {
		if a.syncOutages[a.file] {
			delete(a.syncOutages, a.file)
			a.note(chatlist.SyncRestored)
		}
		return
	}
	if period <= 0 || a.now().Sub(since) <= period || a.syncOutages[a.file] {
		return
	}
	if a.syncOutages == nil {
		a.syncOutages = map[string]bool{}
	}
	a.syncOutages[a.file] = true
	a.note(chatlist.SyncPaused)
}
