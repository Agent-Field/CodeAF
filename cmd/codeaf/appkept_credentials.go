package main

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
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

type appKeptCredentials struct {
	mu      sync.Mutex
	pending []appKeptCredential
}

type appKeptCredential struct {
	change appwire.CredentialChange
	at     time.Time
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
			if p.change.Provider != provider && now.Sub(p.at) < appwire.CredentialsTakeLife {
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
	// The announcement carries nothing, so it goes after the lock is let go.
	guard.Go("app/credentials changed", func() { b.emit.Emit(appwire.EventCredentialsChanged, appwire.Empty{}) })
}

// CredentialsTake drains the changes not yet taken and not yet expired, each
// answered once.
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
	return out, nil
}
