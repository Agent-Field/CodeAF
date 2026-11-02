package main

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// ── THE SIGN-INS THE APP KEEPS (internal/appwire/factory_sessions.go) ──────
//
// The hub holds a team factory's sign-in in memory. What the app should keep
// for the next launch is queued here, one change per connection with the
// latest winning, and the app's core drains it with factory.sessions_take
// after factory.sessions_changed says there is something to take. The
// machine's own factory is never queued: its key is made again at every start.

var _ appwire.FactorySessionKeeper = (*appBackend)(nil)

// keptChange is one queued change and when it was queued.
type keptChange struct {
	change appwire.FactorySessionChange
	at     time.Time
}

// keepSessionLocked queues id's sign-in to be kept. Callers hold h.mu.
func (h *appFactoryHub) keepSessionLocked(id, url string, s appFactorySession) {
	if id == appLocalID || url == "" || s.token == "" {
		return
	}
	c := appwire.FactorySessionChange{Factory: id, URL: url, Token: s.token}
	if !s.expires.IsZero() {
		at := s.expires
		c.Expires = &at
	}
	h.queueLocked(c)
}

// forgetKeptLocked queues id's kept sign-in to be removed. Callers hold h.mu.
func (h *appFactoryHub) forgetKeptLocked(id string) {
	if id == appLocalID {
		return
	}
	h.queueLocked(appwire.FactorySessionChange{Factory: id, Removed: true})
}

func (h *appFactoryHub) queueLocked(c appwire.FactorySessionChange) {
	if h.closed {
		return
	}
	now := h.now()
	kept := h.kept[:0]
	for _, k := range h.kept {
		if k.change.Factory != c.Factory && now.Sub(k.at) < appwire.FactorySessionsTakeLife {
			kept = append(kept, k)
		}
	}
	// A FULL QUEUE DROPS ITS OLDEST SIGN-IN TO KEEP, never a removal: a removal
	// lost would leave a dead item kept, and a lost keep only costs a sign-in.
	if len(kept) >= appwire.FactorySessionsMost {
		drop := 0
		for i, k := range kept {
			if !k.change.Removed {
				drop = i
				break
			}
		}
		kept = append(kept[:drop], kept[drop+1:]...)
	}
	h.kept = append(kept, keptChange{change: c, at: now})
	// The announcement carries nothing, so it may go after the lock is let go.
	if emit := h.keptChanged; emit != nil {
		guard.Go("app/factory sessions changed", emit)
	}
}

// FactorySessionsTake drains the changes not yet taken and not yet expired.
func (b *appBackend) FactorySessionsTake(context.Context) (appwire.FactorySessionsTaken, error) {
	h := b.factoryHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	out := appwire.FactorySessionsTaken{Changes: []appwire.FactorySessionChange{}}
	for _, k := range h.kept {
		if now.Sub(k.at) < appwire.FactorySessionsTakeLife {
			out.Changes = append(out.Changes, k.change)
		}
	}
	h.kept = nil
	return out, nil
}

// FactorySessionRestore takes back a sign-in the app kept, for a saved
// connection at the same address that is not this machine's own factory. It is
// not queued to be kept again, and a refusal later forgets it once.
func (b *appBackend) FactorySessionRestore(_ context.Context, p appwire.FactorySessionRestoreParams) (appwire.FactorySessionRestored, error) {
	h := b.factoryHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return appwire.FactorySessionRestored{}, appwire.Errorf(appwire.CodeClosed, "the engine is stopping")
	}
	if err := h.load(); err != nil {
		return appwire.FactorySessionRestored{}, err
	}
	if p.Factory == appLocalID {
		return appwire.FactorySessionRestored{}, nil
	}
	s := appFactorySession{token: p.Token, restored: true}
	if p.Expires != nil {
		s.expires = *p.Expires
		if !h.now().Before(s.expires) {
			return appwire.FactorySessionRestored{}, nil
		}
	}
	for _, row := range h.rows {
		if row.ID != p.Factory || row.Local {
			continue
		}
		if row.URL != p.URL {
			return appwire.FactorySessionRestored{}, nil
		}
		redact.Register(p.Token)
		trace.Secret(p.Token)
		// A sign-in made in this run is newer than the one kept from the last.
		if h.tokenLocked(row.ID) == "" {
			h.sessions[row.ID] = s
		}
		return appwire.FactorySessionRestored{Kept: true}, nil
	}
	return appwire.FactorySessionRestored{}, nil
}

// refused is [appFactoryHub.refusedLocked] for a caller that does not hold h.mu.
func (h *appFactoryHub) refused(id, token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refusedLocked(id, token)
}

// refusedLocked forgets a restored sign-in the factory refused, if it is still
// token, so a sign-in kept from an earlier launch that has died reads signed
// out once, is removed from the app's keeping, and is never sent again. A
// sign-in made in this run is left as it always was. Callers hold h.mu.
func (h *appFactoryHub) refusedLocked(id, token string) {
	if s, ok := h.sessions[id]; ok && s.restored && s.token == token && token != "" {
		delete(h.sessions, id)
		h.dropHeldLocked(id)
		h.forgetKeptLocked(id)
	}
}
