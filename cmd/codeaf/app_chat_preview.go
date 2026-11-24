package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type peerChatTitle struct {
	holder, head, title string
	refresh             time.Time
}

const (
	peerTitleBudget   = 1200 * time.Millisecond
	peerTitleRefresh  = 20 * time.Second
	chatPreviewWithin = 12 * time.Second
)

// appCanLive says a chat may be opened live: another computer is running it, is
// online and is named, and remote control is on here. With it off no row says
// anything about live at all.
func appCanLive(row appwire.ChatRow, fact chatlist.Row, online bool) bool {
	return !row.Here && row.Status == string(chatlist.Running) && online && fact.DeviceID != "" && config.RemoteControlOn()
}

// describeChatRows keeps directory projections intact while filling titles from
// online peers. The inventory lock serializes it with moves and other reads.
func (c *appChats) describeChatRows(ctx context.Context, s *syncsetup.Sync, rows []appwire.ChatRow, facts map[string]chatlist.Row) {
	live, storage := s.LiveTransfer(), s.StorageAllowed()
	binding := s.Relay + "/" + s.Identity.ID() + "/" + s.Device.ID()
	if !live || binding != c.titleBinding {
		c.peerTitles = nil
		c.titleBinding = binding
	}
	if live && c.peerTitles == nil {
		c.peerTitles = make(map[string]peerChatTitle)
	}
	for id := range c.peerTitles {
		if _, ok := facts[id]; !ok {
			delete(c.peerTitles, id)
		}
	}
	now := time.Now()
	groups := make(map[string][]string)
	for i := range rows {
		row := &rows[i]
		fact := facts[row.Cell]
		online := !fact.HolderRemoved && (row.Online == nil || *row.Online)
		row.CanPreview = !row.Here && fact.Head != "" && (storage || live && online)
		// A CHAT ANOTHER COMPUTER IS RUNNING CAN BE OPENED LIVE while remote
		// control is on (app_live_open.go); with it off the row says nothing.
		row.CanLive = appCanLive(*row, fact, online)
		if row.Here {
			continue
		}
		row.Title = fact.Title
		if !live || fact.DeviceID == "" || fact.Head == "" {
			continue
		}
		old := c.peerTitles[row.Cell]
		if old.holder == fact.DeviceID && old.title != "" {
			row.Title = old.title
		}
		if online && (old.holder != fact.DeviceID || old.head != fact.Head || !now.Before(old.refresh)) {
			groups[fact.DeviceID] = append(groups[fact.DeviceID], row.Cell)
		}
	}
	if len(groups) == 0 {
		return
	}
	ctx, stop := context.WithTimeout(ctx, peerTitleBudget)
	defer stop()
	for holder, ids := range groups {
		for batch := range slices.Chunk(ids, filesdoor.MaxChatTitles) {
			if ctx.Err() != nil {
				break
			}
			titles, err := s.PeerChatTitles(ctx, holder, batch)
			refreshAfter := peerTitleRefresh
			if err != nil && ctx.Err() != nil {
				refreshAfter = syncsetup.DefaultInterval
			}
			for _, id := range batch {
				title := titles[id]
				if old := c.peerTitles[id]; title == "" && old.holder == holder {
					title = old.title
				}
				c.peerTitles[id] = peerChatTitle{holder: holder, head: facts[id].Head, title: title, refresh: now.Add(refreshAfter)}
			}
		}
	}
	for i := range rows {
		row := &rows[i]
		if old := c.peerTitles[row.Cell]; !row.Here && old.holder == facts[row.Cell].DeviceID && old.title != "" {
			row.Title = old.title
		}
	}
}

// PreviewChat reads only the selected saved conversation in temporary scratch.
// It uses the same roster-checked peer transport as a move with storage off.
func (b *appBackend) PreviewChat(id string) (appwire.ChatPreview, error) {
	view, known := b.chats.viewFor(id)
	if !known {
		return appwire.ChatPreview{}, appwire.Errorf(appwire.CodeNotFound, "This chat is no longer listed.")
	}
	s, ok, err := syncsetup.Open(b.relayHome())
	if err != nil {
		return appwire.ChatPreview{}, appEngineError(err)
	}
	if !ok || s.ChatStorageOff() {
		return appwire.ChatPreview{}, appwire.Errorf(appwire.CodeUnreachable, "The saved messages aren't available on this computer.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), chatPreviewWithin)
	defer cancel()
	guard.Go("app/chat-preview-stop", func() {
		select {
		case <-b.left:
			cancel()
		case <-ctx.Done():
		}
	})
	var preview appwire.ChatPreview
	err = s.ReadChat(ctx, id, view, func(c cell.Cell) error {
		if summary, ok := session.Peek(filepath.Join(c.Root, cell.TranscriptPath)); ok {
			preview.LastUser, preview.LastAssistant = summary.LastUser, summary.LastAssistant
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, session.ErrRecordReadBusy) {
			return appwire.ChatPreview{}, appEntriesBusy(err)
		}
		if errors.Is(err, cellsync.ErrTranscriptTooLarge) {
			return appwire.ChatPreview{}, appwire.Errorf(appwire.CodeUnreachable, "This saved conversation is too large to preview. Move here to read its full history.")
		}
		return appwire.ChatPreview{}, appwire.Errorf(appwire.CodeUnreachable, "Couldn't load the saved messages. Check that the other computer is running and try again.")
	}
	return preview, nil
}
