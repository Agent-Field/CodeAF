package tui3

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// renewalReader reports the first failed renewal, its period, and whether this
// conversation still holds the chat. A successful renewal clears the time.
// The reading is local and never waits for the network.
type renewalReader interface {
	RenewalStatus() (time.Time, time.Duration, bool)
}

// takeRenewalNotice says each sustained outage and recovery once for its chat.
func (a *app) takeRenewalNotice() {
	source, ok := a.agent.(renewalReader)
	if !ok {
		return
	}
	since, period, held := source.RenewalStatus()
	if !held {
		delete(a.renewalFailures, a.file)
		return
	}
	if since.IsZero() {
		if a.renewalFailures[a.file] {
			a.note(chatlist.SyncRestored)
		}
		return
	}
	if a.renewalFailures == nil {
		a.renewalFailures = map[string]bool{}
	}
	a.renewalFailures[a.file] = true
	if period <= 0 || a.now().Sub(since) <= period || a.syncOutages[a.file] {
		return
	}
	if a.syncOutages == nil {
		a.syncOutages = map[string]bool{}
	}
	if a.syncService != "" {
		a.note(syncwords.Outage(a.syncService))
	} else {
		a.note(chatlist.SyncPaused)
	}
	// Accepting a new outage clears older evidence, so retain this observation
	// only after the notice has established the current outage.
	a.renewalFailures[a.file] = true
}

// acceptSyncNotice shares the chat's existing outage ledger across producers.
func (a *app) acceptSyncNotice(text string) bool {
	if text == syncwords.Recovered || text == chatlist.SyncRestored {
		had := a.syncOutages[a.file]
		delete(a.syncOutages, a.file)
		delete(a.renewalFailures, a.file)
		return had
	}
	outage := syncwords.IsOutage(text)
	if !outage {
		return true
	}
	if a.syncOutages[a.file] {
		return false
	}
	if a.syncOutages == nil {
		a.syncOutages = map[string]bool{}
	}
	a.syncOutages[a.file] = true
	delete(a.renewalFailures, a.file)
	return true
}

// chatNotice chooses the recovery door and outage sentence for this surface.
func (a *app) chatNotice(text string) string {
	if chatlist.IsRemovedNotice(text) {
		return chatlist.RemovedChat
	}
	if text == chatlist.SyncPaused || text == syncwords.LegacyOutage {
		if a.syncService != "" {
			return syncwords.Outage(a.syncService)
		}
		return chatlist.SyncPaused
	}
	return text
}
