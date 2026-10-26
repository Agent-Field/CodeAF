package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
	"time"
)

// Status is a side read. Persist the human exchange separately so it cannot
// enter a provider tool batch, claim/finish a turn or consume pending steering.
func (b *appBackend) tryFactoryStatus(a *appAgent, text, clientID string, shown *appSending) (uint64, bool, error) {
	if !factory.StatusOnlyRequest(text) || (shown != nil && (len(shown.attachments) > 0 || len(shown.described) > 0 || len(shown.reads) > 0 || shown.composed != nil || strings.TrimSpace(shown.text) != strings.TrimSpace(text))) {
		return 0, false, nil
	}
	live := b.liveOf(a)
	if live == nil {
		return 0, false, nil
	}
	door, ok := live.conn.Conv.(interface {
		ReadFactoryStatus(context.Context, string) (string, bool, error)
	})
	if !ok {
		return 0, false, nil
	}
	if err := b.workAllowed(a); err != nil {
		return 0, true, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply, handled, err := door.ReadFactoryStatus(ctx, text)
	if !handled {
		return 0, false, nil
	}
	if err != nil {
		return 0, true, appEngineError(err)
	}
	if err := b.knownWorkRefusal(a); err != nil {
		return 0, true, err
	}
	var id string
	var turn uint64
	b.locked(func() { id = a.view.ID; turn = live.turn })
	key := clientID
	if key == "" {
		key = fmt.Sprintf("%d", b.turns.Add(1))
	}
	key = fmt.Sprintf("factory-status:%x", sha256.Sum256([]byte(id+":"+key)))
	entries := []appwire.Entry{
		{ID: key + ":user", Agent: id, Kind: appwire.EntryUser, At: b.now(), Text: text, ClientID: clientID},
		{ID: key + ":answer", Agent: id, Kind: appwire.EntryAnswer, At: b.now(), Text: reply, NoticeKind: "factory_status"},
	}
	if err := b.keepAppEntries(a, entries, ""); err != nil {
		return 0, true, appEngineError(err)
	}
	for _, e := range entries {
		b.emitAppFooter(a, e.ID)
	}
	return turn, true, nil
}
