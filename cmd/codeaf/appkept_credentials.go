package main

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// ── THE CREDENTIALS THE APP KEEPS (internal/appwire/credentials.go) ───────
//
// A credential a door gave this launch (config.AppCredentials.Keep) is queued
// here, one change per provider with the latest winning, and the app's core
// drains it with credentials.take after credentials.changed says there is
// something to take. A key the app handed back with keys.set is held with
// Set, which queues nothing.

var _ appwire.CredentialKeeper = (*appBackend)(nil)
var _ appwire.CredentialSyncHost = (*appBackend)(nil)

type appKeptCredentials struct {
	mu         sync.Mutex
	pending    []appKeptCredential
	synced     map[string]appSyncedCredential
	nextTicket uint64
}

type appKeptCredential struct {
	change appwire.CredentialChange
	at     time.Time
}

type appSyncedCredential struct {
	change  appwire.CredentialChange
	before  config.AppKeyState
	at      time.Time
	wait    time.Duration
	retryAt time.Time
	taken   bool
}

func (b *appBackend) CredentialsReady() {
	b.keys.MarkSyncReady()
	b.chats.wakePublication()
}

func (b *appBackend) queueSyncedCredential(provider string, before config.AppKeyState, key string) {
	queued := func() bool {
		k := &b.credentials
		k.mu.Lock()
		defer k.mu.Unlock()
		if b.keys.State(provider) != before {
			return false
		}
		if k.synced == nil {
			k.synced = make(map[string]appSyncedCredential)
		}
		now := b.now()
		if previous, ok := k.synced[provider]; ok && now.Sub(previous.at) < appwire.CredentialsTakeLife &&
			previous.before == before && (previous.taken || previous.change.Key == key) {
			return false
		}
		k.nextTicket++
		k.synced[provider] = appSyncedCredential{
			change: appwire.CredentialChange{Provider: provider, Key: key, Removed: key == "", ExpectedKey: &before.Key, Ticket: k.nextTicket},
			before: before, at: now,
		}
		return true
	}()
	if queued {
		b.announceCredential()
	}
}

// queueCredential is config.AppCredentials' listener: key "" is a removal.
func (b *appBackend) queueCredential(provider, key string) {
	c := appwire.CredentialChange{Provider: provider, Key: key, Removed: key == ""}
	func() {
		k := &b.credentials
		k.mu.Lock()
		defer k.mu.Unlock()
		now := b.now()
		kept := k.pending[:0]
		for _, p := range k.pending {
			if now.Sub(p.at) >= appwire.CredentialsTakeLife {
				continue
			}
			if p.change.Provider != provider {
				kept = append(kept, p)
			}
		}
		// A FULL QUEUE DROPS ITS OLDEST KEY TO KEEP, never a removal: a lost
		// removal would leave a dead key kept, a lost keep only costs a paste.
		if len(kept) >= appwire.CredentialsMost {
			drop := 0
			for i, p := range kept {
				if !p.change.Removed {
					drop = i
					break
				}
			}
			kept = append(kept[:drop], kept[drop+1:]...)
		}
		k.pending = append(kept, appKeptCredential{change: c, at: now})
	}()
	b.credentialChanged(provider)
	b.announceCredential()
}

func (b *appBackend) credentialChanged(provider string) {
	if p, ok := b.appProviderNamed(provider); ok {
		b.keysMoved(provider)
		// Native storage takes the key through credentials.take. Provider events
		// only refresh the already-open account and model views.
		b.providerChanged(p, "")
		b.warmProviderModels()
	}
}

func (b *appBackend) announceCredential() {
	// The announcement carries nothing, so it goes after the lock is let go.
	guard.Go("app/credentials changed", func() { b.emit.Emit(appwire.EventCredentialsChanged, appwire.Empty{}) })
}

// A guarded write can be replayed if the take response or its acknowledgement
// is lost. A keeper that already made the write recognizes the same value.
const appCredentialAckWait = 5 * time.Second

func (b *appBackend) announceCredentialWhenDue(provider string, proposal appSyncedCredential, now time.Time) {
	ticket, due := proposal.change.Ticket, proposal.retryAt
	if due.Sub(proposal.at) >= appwire.CredentialsTakeLife {
		return
	}
	b.after(due.Sub(now), func() {
		if b.isClosing() {
			return
		}
		k := &b.credentials
		k.mu.Lock()
		current, exists := k.synced[provider]
		now := b.now()
		waiting := exists && current.change.Ticket == ticket && current.retryAt.Equal(due) &&
			!now.Before(due) && now.Sub(current.at) < appwire.CredentialsTakeLife && b.keys.State(provider) == current.before
		k.mu.Unlock()
		if waiting {
			b.announceCredential()
		}
	})
}

// CredentialsTake drains ordinary changes once. A synced ticket remains
// available after its acknowledgement deadline until confirmed or expired.
func (b *appBackend) CredentialsTake(context.Context) (appwire.CredentialsTaken, error) {
	k := &b.credentials
	k.mu.Lock()
	defer k.mu.Unlock()
	now := b.now()
	out := appwire.CredentialsTaken{Changes: []appwire.CredentialChange{}}
	for _, p := range k.pending {
		if now.Sub(p.at) < appwire.CredentialsTakeLife {
			out.Changes = append(out.Changes, p.change)
		}
	}
	k.pending = nil
	for provider, p := range k.synced {
		if now.Sub(p.at) >= appwire.CredentialsTakeLife || b.keys.State(provider) != p.before {
			delete(k.synced, provider)
			continue
		}
		if !now.Before(p.retryAt) {
			p.taken = true
			p.retryAt = now.Add(appCredentialAckWait)
			k.synced[provider] = p
			out.Changes = append(out.Changes, p.change)
			b.announceCredentialWhenDue(provider, p, now)
		}
	}
	return out, nil
}

func (b *appBackend) CredentialsConfirm(ticket uint64, applied bool) {
	k := &b.credentials
	k.mu.Lock()
	var proposal appSyncedCredential
	for provider, p := range k.synced {
		if p.change.Ticket != ticket || !p.taken {
			continue
		}
		if applied || b.keys.State(provider) != p.before {
			delete(k.synced, provider)
			proposal = p
		} else {
			now := b.now()
			p.taken = false
			p.wait = min(15*time.Second, max(time.Second, 2*p.wait))
			p.retryAt = now.Add(p.wait)
			k.synced[provider] = p
			b.announceCredentialWhenDue(provider, p, now)
		}
		break
	}
	k.mu.Unlock()
	if proposal.change.Ticket != 0 && applied && b.keys.AcceptSynced(proposal.change.Provider, proposal.before, proposal.change.Key) {
		// Another vault value may have arrived while this ticket was taken.
		b.keys.RequestSyncRefresh()
		b.credentialChanged(proposal.change.Provider)
	}
}
