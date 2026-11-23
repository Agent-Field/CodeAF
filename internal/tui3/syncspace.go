package tui3

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// syncSpaceWithin keeps an optional relay reading from outliving its usefulness.
const syncSpaceWithin = 2 * time.Second

// syncSpaceMsg carries one answer from the relay's store stats. A failed or
// timed-out reading arrives as empty counts, which draws nothing.
type syncSpaceMsg struct {
	counts blobstore.Counts
}

// runStatusCommand answers /status AT ONCE from the last reading and then asks
// the relay again in the background for the next one. /status used to answer
// instantly, and a network round trip in front of it would make an offline
// laptop wait on every press; the stored amount changes slowly, so a reading
// taken at launch or at the previous /status is close enough.
func (a *app) runStatusCommand(rest string) tea.Cmd {
	refresh := a.refreshSyncSpace()
	a.showStatus(rest == "--json")
	return refresh
}

// refreshSyncSpace is the one background reading, or nil when sync is off or no
// relay can report it. It is a command, so it never runs on the update loop or
// while a frame is painted.
func (a *app) refreshSyncSpace() tea.Cmd {
	// Pairing can happen after this surface opened with no sync service.
	if a.syncService == "" {
		if code, _ := a.relayStorageAccount(); code != "" {
			if door, ok := a.approvals.(interface{ SyncService() string }); ok {
				a.syncService = door.SyncService()
			}
		}
	}
	if a.syncService == "" || a.syncService == "off" || a.syncStats == nil {
		a.syncSpace = blobstore.Counts{}
		return nil
	}
	ctx, fetch := a.ctx, a.syncStats
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, syncSpaceWithin)
		defer cancel()
		counts, err := fetch(ctx)
		if err != nil {
			counts = blobstore.Counts{}
		}
		return syncSpaceMsg{counts: counts}
	}
}

func (a *app) showStatus(json bool) {
	if json {
		a.note(a.statusJSON())
		return
	}
	text := a.statusText()
	a.noteFacts(text, a.statusFacts(text)...)
}

// syncSpaceWord reads only the last answer, so rendering cannot contact a relay.
func (a *app) syncSpaceWord() string {
	used, limit := a.syncSpace.StoredBytes, a.syncSpace.LimitBytes
	if a.syncService == "" || a.syncService == "off" || a.relayStorageOff() || used == nil || limit == nil || *used <= 0 || *limit <= 0 {
		return ""
	}
	return chatlist.BytesSize(*used) + " of " + chatlist.BytesSize(*limit)
}

// Failed readings retain only the previous permission, never stale usage totals.
func (a *app) relayStorageOff() bool {
	if a.syncService == "" || a.syncService == "off" {
		return false
	}
	// Another local process or a refused upload may have refreshed permission
	// after the last usage reading was painted.
	if _, state := a.relayStorageAccount(); state != "" {
		return state == "off"
	}
	return a.syncSpace.Storage == "off"
}
