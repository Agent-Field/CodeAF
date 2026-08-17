// Package connect is the accounts layer: it holds the keys that let aforge act
// on a person's own SaaS accounts — their mail, their calendar — and hands the
// rest of the program one ready, self-refreshing [http.Client] per account.
//
// The registry is the design. Google is one plug and not the point — it is the
// plug that happens to exist today. A service that arrives later declares
// itself from its own file's init, and no caller, no settings screen and no
// line of this file changes. A closed switch over service names would put every
// future vendor's vocabulary in this source and make the package a merge point
// for work that has nothing to do with connecting.
//
// The package imports nothing of the surface — no session, no TUI, no config.
// It is given a profile directory to keep its file in and a map of client
// credentials to sign requests with, both as plain values, so every law here
// can be tested without a real account or a real browser.
//
// THE PACKAGE NEVER OPENS A BROWSER. [Manager.BeginAuth] starts the loopback
// listener and returns the address to visit; whoever owns the screen decides
// how that address reaches the person.
//
// THE PACKAGE NEVER LOGS A KEY. No access key, refresh key or client secret is
// printed, returned in an error, or written anywhere but the store file.
package connect

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// ClientCredential is the application's own identity with one service: the
// pair a person registers once in that service's developer console and hands
// to aforge. It is not the person's account — it only lets aforge ask for one.
type ClientCredential struct {
	ID     string
	Secret string
}

// ok reports whether the pair is complete enough to attempt a connection. A
// half-filled credential is treated exactly as a missing one, because the only
// thing it can produce is a failure at the far end of a browser trip.
func (c ClientCredential) ok() bool {
	return strings.TrimSpace(c.ID) != "" && strings.TrimSpace(c.Secret) != ""
}

// The two ways an account is connected, and there are only two.
//
// [AuthBrowser] is a trip through the person's own browser and the service's
// own sign-in page: they are asked there, by the service, in the service's
// words, and what comes back is a set of keys aforge renews for itself. Google
// is one.
//
// [AuthKey] is a key the person already holds and pastes once. Nothing opens,
// nothing renews, and the key is exactly as good as the day it was made. Every
// service the catalog (catalog.go) brings is one of these.
const (
	AuthBrowser = "browser"
	AuthKey     = "key"
)

// Service is what one connectable account looks like on a menu, flat strings
// because that is what gets rendered — not a model of anybody's API.
type Service struct {
	// ID is the stable identifier: the key in the credentials map, the key
	// in the store file, and the argument every method here takes.
	ID string
	// Name is what a person reads, for example "Google".
	Name string
	// Blurb is one short line saying what connecting it buys.
	Blurb string
	// Auth is how this one is connected: [AuthBrowser] or [AuthKey].
	Auth string
	// Address is the root every call to this service is made against, for
	// the services connected with a key. A service whose address is not
	// fully known until it is connected carries the part that is known and
	// the missing piece as a plain word in angle brackets, which is a thing
	// a person can read rather than a thing that looks like an address.
	//
	// A service connected through the browser leaves it empty: it has no one
	// address, and THE EMPTINESS LAW says an unknown is empty.
	Address string
	// Category is the one word a catalog of two hundred services is browsed
	// by — "billing", "crm", "calls & meetings" — filled from catalog
	// metadata by a later wiring wave. EMPTY IS THE HONEST DEFAULT and every
	// reader treats it as "other", so a build whose catalog says nothing
	// about categories draws the flat list it drew before this field existed.
	Category string
	// Scopes are the permissions asked for. They are listed here so a
	// screen can say plainly what it is about to request.
	Scopes []string
}

// Status is a [Service] plus where it stands right now.
type Status struct {
	Service
	// Connected reports that aforge holds usable keys for this service.
	Connected bool
	// Account is the address the keys belong to. THE EMPTINESS LAW: an
	// account we do not know is empty, never a placeholder — a screen that
	// renders nothing is honest, one that renders "unknown" is not.
	Account string
}

// Plug is one connectable service. A plug is a value with no state of its own:
// it describes a service and knows the two things that differ between vendors,
// which are where the browser trip goes and how to ask the service whose
// account this is.
type Plug interface {
	// Service describes the plug for a menu. It must be cheap and constant.
	Service() Service
	// Endpoint is where the browser trip goes and where the exchange lands.
	Endpoint() oauth2.Endpoint
	// AuthCodeOptions are the extra request parameters this vendor needs on
	// the way out, beyond the ones every plug gets (a proof key, and the
	// permissions from Service). Google needs offline access and a forced
	// consent screen; another vendor may need nothing.
	AuthCodeOptions() []oauth2.AuthCodeOption
	// Account answers whose account the given client is acting for, as an
	// address a person recognises. Returning an error is ordinary and
	// tolerated: the caller stores an empty account and moves on.
	Account(ctx context.Context, client *http.Client) (string, error)
}

var (
	registryMu sync.RWMutex
	registry   []Plug
)

// Register adds a plug. Meant to be called from a package file's init, which is
// why it panics rather than returning an error: a plug that failed to register
// would not fail at registration but silently later, by being absent from a
// menu nobody thought to check.
func Register(p Plug) {
	if p == nil {
		panic("connect: register nil plug")
	}
	if strings.TrimSpace(p.Service().ID) == "" {
		panic("connect: register plug with empty id")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = append(registry, p)
}

// Registered lists every plug built into this binary, connected or not, in the
// same order [Manager.Services] uses.
//
// TWO PLUGS CANNOT SHARE AN ID, AND THE HAND-WRITTEN ONE WINS. The catalog
// (catalog.go) brings hundreds of services from somebody else's list, and a
// service written by hand in this package may one day turn up on that list too.
// The hand-written plug is the one with a browser trip, named permissions and
// real tools behind it; the catalog's row for the same service would offer
// strictly less under the same name, and two rows with one id would make every
// lookup a coin flip.
func Registered() []Plug {
	registryMu.RLock()
	plugs := append([]Plug(nil), registry...)
	registryMu.RUnlock()

	held := make(map[string]int, len(plugs))
	kept := make([]Plug, 0, len(plugs))
	for _, plug := range plugs {
		service := plug.Service()
		at, seen := held[service.ID]
		if !seen {
			held[service.ID] = len(kept)
			kept = append(kept, plug)
			continue
		}
		if kept[at].Service().Auth == AuthKey && service.Auth != AuthKey {
			kept[at] = plug
		}
	}
	sortPlugs(kept)
	return kept
}

// sortPlugs puts the plugs in the order a menu shows them.
//
// THE ORDER IS THE NAME'S ORDER, not the order the plugs registered in. Go runs
// a package's init functions in filename order, so a menu that followed
// registration would reshuffle the day somebody renamed a source file, and a
// menu that reshuffles is a menu nobody can build a habit on.
func sortPlugs(plugs []Plug) {
	sort.SliceStable(plugs, func(i, j int) bool {
		left, right := plugs[i].Service(), plugs[j].Service()
		if a, b := strings.ToLower(left.Name), strings.ToLower(right.Name); a != b {
			return a < b
		}
		return left.ID < right.ID
	})
}

// Manager is the one handle the rest of the program holds: it owns the store
// file, the client credentials, and every connection made with them.
type Manager struct {
	store *store
	creds map[string]ClientCredential
	plugs []Plug
}

// NewManager opens the store under profileDir and binds it to the credentials
// the caller was configured with. An empty profileDir means aforge's own state
// root, matching every other file the program keeps.
//
// Opening reads the store once so that a damaged file is an error here, at
// startup, rather than a surprise in the middle of a person's first connection.
func NewManager(profileDir string, creds map[string]ClientCredential) (*Manager, error) {
	s := newStore(profileDir)
	if _, err := s.load(); err != nil {
		return nil, err
	}
	m := &Manager{
		store: s,
		creds: make(map[string]ClientCredential, len(creds)),
		plugs: Registered(),
	}
	// The map is copied so that a caller mutating theirs afterwards cannot
	// change which services this manager believes it can offer.
	for id, c := range creds {
		m.creds[id] = c
	}
	return m, nil
}

// Services lists what this build can connect, in a stable order.
//
// A service with no client credential is not listed at all. It is not shown
// greyed out, and it does not appear with an explanation of what to register
// where: a menu entry that cannot be chosen is a menu entry that wastes the
// reader's attention. A service connected with a key needs no such credential
// and is always listed — the person's own key is the whole of what it takes.
//
// THE FILE IS READ ONCE HERE. There are hundreds of services on this list and
// one shared file behind them; asking the file about each service in turn would
// read it hundreds of times to answer one question.
func (m *Manager) Services() []Status {
	entries, err := m.store.load()
	if err != nil {
		// A store that has become unreadable reads as nothing connected, for
		// [Manager.standing]'s reason.
		entries = map[string]stored{}
	}
	out := make([]Status, 0, len(m.plugs))
	for _, p := range m.plugs {
		service := p.Service()
		if !m.offered(service) {
			continue
		}
		status := Status{Service: service}
		if record, ok := entries[service.ID]; ok && record.usable() && record.covers(service.Scopes) {
			status.Connected = true
			status.Account = record.Account
			if address, err := located(p, record); err == nil {
				status.Address = address
			}
		}
		out = append(out, status)
	}
	return out
}

// offered reports whether this build can put the service in front of a person
// at all: a browser service needs the client credential it was configured with,
// and a key service needs nothing but the person.
func (m *Manager) offered(service Service) bool {
	return service.Auth == AuthKey || m.creds[service.ID].ok()
}

// Connected reports whether id can be used right now.
//
// Connected means ALL THREE things are true: the client credential this build
// was configured with, stored keys for a person's account, and a grant that
// covers what the service asks for today. Any one of them missing is a service
// that cannot do the work the caller is about to ask for.
func (m *Manager) Connected(id string) bool {
	plug, err := m.plug(id)
	if err != nil {
		return false
	}
	_, ok := m.standing(plug)
	return ok
}

// standing is the one reading of "is this connected", and every caller goes
// through it so that a menu, a tool call and a client can never disagree.
//
// A store that has become unreadable since startup reads as nothing being
// connected, which is the safe answer: it makes the person sign in again rather
// than letting a screen promise access aforge cannot deliver.
func (m *Manager) standing(p Plug) (stored, bool) {
	service := p.Service()
	if !m.offered(service) {
		return stored{}, false
	}
	record, ok, err := m.store.get(service.ID)
	if err != nil || !ok || !record.usable() || !record.covers(service.Scopes) {
		return stored{}, false
	}
	return record, true
}

// Disconnect forgets a service: the stored keys go, the client credential and
// the menu entry stay, so the service can be connected again without any
// further setup.
//
// Disconnect on something that was never connected succeeds. The caller asked
// for a state, the state holds, and an error there would only be an error about
// bookkeeping.
func (m *Manager) Disconnect(id string) error {
	plug, err := m.plug(id)
	if err != nil {
		return err
	}
	if err := m.store.remove(plug.Service().ID); err != nil {
		return fmt.Errorf("disconnect %s: %w", plug.Service().Name, err)
	}
	return nil
}

// plug finds the plug for id, or says plainly that this build has no such
// service.
func (m *Manager) plug(id string) (Plug, error) {
	id = strings.TrimSpace(id)
	for _, p := range m.plugs {
		if p.Service().ID == id {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no service named %q", id)
}

// credential finds the client credential for id, or says that this build was
// not set up to offer the service — deliberately without naming a file, an
// environment variable or a console, none of which this package knows about.
func (m *Manager) credential(id string) (ClientCredential, error) {
	c := m.creds[id]
	if !c.ok() {
		return ClientCredential{}, fmt.Errorf("%s is not available in this build", id)
	}
	return c, nil
}

// config assembles the request settings for one plug. The redirect address is
// deliberately left empty: the loopback listener picks a port at connect time
// and fills it in, so no port is baked into anything stored.
func (m *Manager) config(p Plug, c ClientCredential) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     strings.TrimSpace(c.ID),
		ClientSecret: strings.TrimSpace(c.Secret),
		Endpoint:     p.Endpoint(),
		Scopes:       append([]string(nil), p.Service().Scopes...),
	}
}
