package main

import (
	"context"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func (*appBackend) ChatReadsFollow() bool { return true }

// remoteEntries is a read-only projection of a shared head. It deliberately
// uses no local call log or turn marks: those belong to the copy left here.
func (b *appBackend) remoteEntries(a *appAgent, before string, limit int) (page []appwire.Entry, more bool, head string, err error) {
	var agent appwire.Agent
	b.locked(func() { agent = a.view })
	// A move notice can update ownership before the agent has the directory's
	// cell and head. Use the same location as the agent event the reader saw.
	b.dressChatLocation(&agent)
	location := agent.Location
	if location == nil || location.Here || location.Cell == "" {
		return
	}
	view, known := b.chats.viewFor(location.Cell)
	if !known || view.Cell.Head == "" {
		if location.Seen != nil && location.Seen.Head != "" {
			err = appwire.Errorf(appwire.CodeUnreachable, "The saved messages aren't available on this computer.")
		}
		return
	}
	s, ok, openErr := syncsetup.Open(b.relayHome())
	if openErr != nil || !ok || s.ChatStorageOff() || view.Cell.Lease.Device == s.Device.ID() {
		err = appwire.Errorf(appwire.CodeUnreachable, "The saved messages aren't available on this computer.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
	defer cancel()
	// Leaving the app stops a fetch as well as its subsequent record read.
	done := make(chan struct{})
	defer close(done)
	guard.Go("app/remote-chat-read-stop", func() {
		select {
		case <-b.left:
			cancel()
		case <-done:
		}
	})
	err = s.ReadChat(ctx, location.Cell, view, func(c cell.Cell) error {
		return appRecords.WithRecord(filepath.Join(c.Root, cell.TranscriptPath), func(record session.Record) error {
			var readErr error
			page, more, readErr = appEntryPage(agent.ID, record, agent.UpdatedAt, nil, before, limit, nil)
			return readErr
		})
	})
	if err == nil {
		head = view.Cell.Head
	}
	return
}
