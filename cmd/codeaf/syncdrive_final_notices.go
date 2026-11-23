package main

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// closeSaved is called after the owner's journal has closed. Storage can be
// off from launch, with no remote writer to give the usual closing receipt.
func (l *liveDrive) closeSaved(ctx context.Context) error {
	if err := l.Close(ctx); err != nil {
		if l.savedLocally != nil && syncwords.SavedWithStorageOff(err) {
			l.savedLocally(quitStorageOffWord)
			return nil
		}
		return err
	}
	if l.localGate == nil || l.localGate.superseded() || !syncsetup.ChatStorageOffForHome(home.Dir()) {
		return nil
	}
	if drive := l.started(); drive != nil {
		if _, viewer := drive.Viewer(); viewer {
			return nil
		}
	}
	if err := cellstore.Await(ctx, l.localGate.cell); err != nil {
		return err
	}
	if spoken, _ := session.ConversationIn(l.transcript, -1); !spoken {
		return nil
	}
	if l.savedLocally != nil {
		l.savedLocally(quitStorageOffWord)
	} else if l.notices != nil {
		l.notices.say(quitStorageOffWord)
	}
	return nil
}

// beginClose keeps final publish news after the last live watch has retired.
func (n *driveNotices) beginClose() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closing = true
}

func (l *liveDrive) TakeSavingNotices() []string {
	if l == nil || l.notices == nil {
		return nil
	}
	n := l.notices
	n.mu.Lock()
	defer n.mu.Unlock()
	lines := n.final
	n.final = nil
	return lines
}
