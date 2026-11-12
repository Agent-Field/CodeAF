package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// Status is a side read. Persist the human exchange separately so it cannot
// enter a provider tool batch, claim/finish a turn or consume pending steering.
func (b *appBackend) tryFactoryStatus(a *appAgent, text, clientID string, shown *appSending) (uint64, bool, error) {
	if !factory.StatusOnlyRequest(text) || !plainFactoryMessage(text, shown) {
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
	return b.keepFactoryExchange(a, text, clientID, "factory-status", reply)
}

func plainFactoryMessage(text string, shown *appSending) bool {
	return shown == nil || (len(shown.attachments) == 0 && len(shown.described) == 0 && len(shown.reads) == 0 &&
		shown.composed == nil && strings.TrimSpace(shown.text) == strings.TrimSpace(text))
}

// A saved factory exchange has no model turn. The desktop handles this receipt
// like a status reply so the composer settles without a turn-started event.
func (b *appBackend) keepFactoryExchange(a *appAgent, text, clientID, kind, reply string) (uint64, bool, error) {
	var id string
	var turn uint64
	b.locked(func() {
		id = a.view.ID
		if a.live != nil {
			turn = a.live.turn
		}
	})
	key := clientID
	if key == "" {
		key = fmt.Sprintf("%d", b.turns.Add(1))
	}
	key = factoryExchangeKey(id, kind, key)
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
	if err := b.touchJobConversation(a.transcript); err != nil {
		return turn, true, &appUnsureSend{err: appEngineError(err)}
	}
	return turn, true, nil
}

func factoryExchangeKey(agent, kind, clientID string) string {
	return fmt.Sprintf("%s:%x", kind, sha256.Sum256([]byte(agent+":"+clientID)))
}
