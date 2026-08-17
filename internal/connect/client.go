package connect

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// clientTimeout is the outer backstop on one request to a service. The real
// per-call deadline is the context the caller passes; this only stops a request
// nobody bounded from hanging for the life of the process.
const clientTimeout = 60 * time.Second

// Client hands back an HTTP client that speaks for the person's account on the
// named service. Every request it makes carries the connection, and a
// connection that has aged out is renewed underneath the caller without them
// asking.
//
// THE RENEWAL IS WRITTEN DOWN. A service is free to hand back a new long-lived
// key when it renews the short-lived one, and a program that used the new key
// but kept the old one on disk works beautifully until it is restarted and then
// asks the person to connect again for no reason they can see. The stock
// renewal machinery offers no way to be told, so the source is wrapped here and
// the keys are compared after every renewal.
func (m *Manager) Client(ctx context.Context, id string) (*http.Client, error) {
	plug, err := m.plug(id)
	if err != nil {
		return nil, err
	}
	service := plug.Service()
	// The same reading of "connected" every other caller gets, which is what
	// keeps a client from being handed out for a sign-in that no longer covers
	// what this build asks for.
	entry, ok := m.standing(plug)
	if !ok {
		return nil, fmt.Errorf("%s is not connected", service.Name)
	}
	// A key-connected service has nothing to renew and no client credential
	// behind it: the key the person pasted goes on every request and that is
	// the whole of it (key.go).
	if holder, keyed := plug.(keyService); keyed {
		return m.keyClient(holder, entry), nil
	}
	credential, err := m.credential(service.ID)
	if err != nil {
		return nil, err
	}
	source := &persisting{
		base:  m.config(plug, credential).TokenSource(ctx, entry.Keys),
		store: m.store,
		id:    service.ID,
		last:  entry.Keys,
	}
	client := oauth2.NewClient(ctx, source)
	client.Timeout = clientTimeout
	return client, nil
}

// staticClient speaks for one fixed key set with no renewal behind it. It is
// what the identity probe uses in the seconds after a connection is made, when
// the keys are known to be fresh and nothing has been written down yet.
func staticClient(ctx context.Context, keys *oauth2.Token) *http.Client {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(keys))
	client.Timeout = clientTimeout
	return client
}

// persisting is the wrapper that notices a renewal and records it.
type persisting struct {
	base  oauth2.TokenSource
	store *store
	id    string

	mu   sync.Mutex
	last *oauth2.Token
}

// Token renews if it must, and writes the result down if anything changed.
//
// A save that fails does NOT fail the caller's request: the keys in hand are
// good, and refusing to do the work the person asked for because a file could
// not be written would turn a bookkeeping problem into a broken feature. The
// remembered key set is left untouched instead, so the very next request tries
// the save again.
func (p *persisting) Token() (*oauth2.Token, error) {
	fresh, err := p.base.Token()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// A service that renews without issuing a new long-lived key expects the
	// old one to keep working, so it is carried forward rather than dropped.
	if strings.TrimSpace(fresh.RefreshToken) == "" && p.last != nil && p.last.RefreshToken != "" {
		carried := *fresh
		carried.RefreshToken = p.last.RefreshToken
		fresh = &carried
	}
	if sameKeys(p.last, fresh) {
		return fresh, nil
	}
	entry, _, err := p.store.get(p.id)
	if err != nil {
		return fresh, nil
	}
	entry.Keys = fresh
	if err := p.store.put(p.id, entry); err != nil {
		return fresh, nil
	}
	p.last = fresh
	return fresh, nil
}

// sameKeys reports whether two key sets are the same one. Comparing the three
// fields that can change is enough, and is the only comparison available: the
// type carries a private extras map that no equality operator will touch.
func sameKeys(a, b *oauth2.Token) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.AccessToken == b.AccessToken &&
		a.RefreshToken == b.RefreshToken &&
		a.Expiry.Equal(b.Expiry)
}
