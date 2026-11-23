package main

import (
	"context"
	"log"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// restartHeldFiles is the app's half of #2781: OPENING A CHAT THIS COMPUTER
// ALREADY HOLDS RESTARTS THE FILES THAT FAILED TO ARRIVE. A chat that moved here
// opens before its files do, and when the window that was fetching them closed
// first, every workspace tool and every save refuses until the files are fetched
// again. For a chat this computer holds there is no take to ask for (the row
// offers plain open and says it cannot be taken), so the open is the restart.
//
// It runs BEFORE the conversation opens, as a take does, because the restart
// asks any window holding the chat's journal to let go, and a conversation
// opened first would be that window. It is [syncsetup.Continuer.RestartFiles],
// the one place the terminal asks too, so it restarts only a failure nobody will
// retry, claims and fences nothing, and leaves a chat another computer holds
// alone.
//
// NOTHING HERE CAN REFUSE THE OPEN. A restart that could not start is said in the
// log and the open goes on, with the failure as it was and its refusal accurate.
func (b *appBackend) restartHeldFiles(id string) {
	a, err := b.find(id)
	if err != nil {
		return
	}
	var transcript string
	b.locked(func() { transcript = a.transcript })
	root, ok := session.FolderOf(transcript)
	if !ok || cell.AbandonedArrival(root) == nil {
		// Most opens end here, with a few stats and no sync service read.
		return
	}
	chat := filepath.Base(root)
	if !b.chats.beginRestart(chat) {
		// Another open of this chat is already starting it.
		return
	}
	defer b.chats.endRestart(chat)
	var live bool
	b.locked(func() { live = a.live != nil })
	if live && !b.releaseIdle(a) {
		// The conversation is open here and busy. The restart asks it to let go of
		// the journal, and a chat in the middle of a turn is not asked to.
		return
	}
	s, ok, err := syncsetup.Open(b.relayHome())
	if err != nil || !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), heldRestartWithin)
	defer cancel()
	say := func(c cell.Cell, line string) { b.syncFact(c, appSyncFact{line: line}) }
	if _, err := chatMover(s, nil, say).RestartFiles(ctx, chat); err != nil {
		log.Printf("open %s: could not restart the files that failed to arrive: %v", chat, err)
	}
}

// beginRestart files a restart of chat's files as running. It answers false while
// one is already running: two opens of one chat must not start two arrivals.
func (c *appChats) beginRestart(chat string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.restarting == nil {
		c.restarting = map[string]bool{}
	}
	if c.restarting[chat] {
		return false
	}
	c.restarting[chat] = true
	return true
}

func (c *appChats) endRestart(chat string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.restarting, chat)
}
