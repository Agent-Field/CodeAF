package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// RegistrationFileName is the file aforge's own identity with each tool server
// lives in, named here so that a doctor screen and a "where are my things"
// answer can say the same word the code reads.
//
// ── WHY IT IS NOT credentials.json ──
//
// What is in credentials.json is one person's keys. What is in here is the
// APPLICATION's: the identity a tool server issued to this copy of aforge when
// it introduced itself, plus the two addresses that sign-in used. For every
// other browser service that identity comes from configuration and is the same
// for everybody running the same build; for these it is minted per machine, so
// it has to be written down somewhere or the next start would ask the vendor for
// a second one and leave the first lying in their console forever.
//
// It is kept at 0600 like the keys and unlike the settings, because a tool
// server may hand back a secret with it.
//
// ── AND WHY IT SURVIVES Disconnect ──
//
// [Manager.Disconnect] is documented as forgetting the person's keys and keeping
// the client credential, so that a service can be reconnected without any
// further setup. This file IS the client credential for these services, so it
// obeys that law by staying: disconnecting Notion and connecting it again is one
// browser trip, not a second registration.
const RegistrationFileName = "toolservers.json"

// mcpRegistration is everything a later start needs to go on using a connection
// this build's sign-in made: who aforge is to that service, where the renewal
// goes, and what the whole thing was bound to.
//
// NO PERSON'S KEY IS IN HERE. The access and refresh keys live with every other
// service's, in the store file.
type mcpRegistration struct {
	// Server is where the service answers, copied from the catalog entry at
	// registration time. A build whose catalog has moved to a new address must
	// not reuse an identity minted for the old one.
	Server string `json:"server"`
	// Issuer is the sign-in this identity belongs to, as that sign-in names
	// itself. IT IS THE KEY TO THE WHOLE RECORD: an identity is only valid at
	// the sign-in that issued it, so a service that changes sign-ins gets a
	// fresh registration rather than a confusing refusal.
	Issuer string `json:"issuer"`
	// Resource is what the keys were asked for and what they are good at —
	// the service's own name for itself, which travels with every request for
	// them so that keys minted for one service cannot be spent at another.
	Resource string `json:"resource"`
	// ClientID is aforge's identity with this sign-in.
	ClientID string `json:"client_id"`
	// ClientSecret is the matching secret, when the sign-in issued one. A
	// sign-in that treats aforge as a public program issues none and this is
	// empty, which is the better outcome and not an error.
	ClientSecret string `json:"client_secret,omitempty"`
	// Authorize and Token are where the browser trip goes and where the
	// exchange and every later renewal land.
	Authorize string `json:"authorize"`
	Token     string `json:"token"`
	// Style is how the sign-in wants aforge to identify itself on a request to
	// the token address, as [oauth2.AuthStyle] spells it.
	Style int `json:"style,omitempty"`
	// Redirects are the loopback addresses this identity was registered with.
	// A connection can only be made on one of them, so an attempt that lands
	// on a port outside this list registers again rather than being bounced by
	// the sign-in with a sentence nobody can act on.
	Redirects []string `json:"redirects,omitempty"`
	// Scopes are the permissions the sign-in was asked for, kept so that a
	// renewal asks for exactly what the grant was made with.
	Scopes []string `json:"scopes,omitempty"`
}

// usable reports whether the record is complete enough to renew a connection
// with. A half-written one is treated as none at all, for [ClientCredential.ok]'s
// reason: the only thing it can produce is a failure at the far end.
func (r mcpRegistration) usable() bool {
	return strings.TrimSpace(r.ClientID) != "" && strings.TrimSpace(r.Token) != ""
}

// fits reports whether an identity already held can be used for a sign-in that
// is about to start: same service, same sign-in, same thing being asked for, and
// a loopback address the sign-in has been told about.
func (r mcpRegistration) fits(server, issuer, resource, redirect string) bool {
	return r.fitsServer(server) &&
		sameIssuer(r.Issuer, issuer) &&
		r.Resource == resource &&
		slices.Contains(r.Redirects, redirect)
}

// fitsServer is the half of [mcpRegistration.fits] that a connection made with
// keys already in hand can check for itself: an identity issued for a service at
// one address says nothing about the same service at another, and a build whose
// catalog has moved must sign in again rather than renew against the old one.
func (r mcpRegistration) fitsServer(server string) bool {
	return r.usable() && r.Server == server
}

// config is the request settings a renewal is made with.
func (r mcpRegistration) config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     r.ClientID,
		ClientSecret: r.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   r.Authorize,
			TokenURL:  r.Token,
			AuthStyle: oauth2.AuthStyle(r.Style),
		},
		Scopes: append([]string(nil), r.Scopes...),
	}
}

// sameIssuer compares two sign-in names the way the specification tolerates: a
// single trailing slash is not a different sign-in, and nothing else about the
// address may differ.
func sameIssuer(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// mcpStore is the file of registrations: a JSON object keyed by service
// identifier, written exactly the way store.go writes its own — read, replace
// one entry, write a temporary file beside it at 0600, rename over the old one.
type mcpStore struct {
	path string
	mu   sync.Mutex
}

var (
	mcpStoresMu sync.Mutex
	mcpStores   = map[string]*mcpStore{}
)

// mcpStoreAt hands back THE store for one directory, building it once, for
// [capabilityStoreAt]'s reason: the lock is only worth anything if every writer
// of that file takes the same one.
func mcpStoreAt(directory string) *mcpStore {
	path := filepath.Join(directory, RegistrationFileName)
	mcpStoresMu.Lock()
	defer mcpStoresMu.Unlock()
	if existing, ok := mcpStores[path]; ok {
		return existing
	}
	fresh := &mcpStore{path: path}
	mcpStores[path] = fresh
	return fresh
}

// registrations names the file this manager keeps its identities in, derived
// from where the connections themselves live for [Manager.policy]'s reason.
func (m *Manager) registrations() *mcpStore {
	return mcpStoreAt(filepath.Dir(m.store.path))
}

// load reads the whole file. A file that is not there yet is no registrations at
// all, which is the ordinary state of a fresh install.
//
// A DAMAGED FILE IS ALSO NO REGISTRATIONS, NEVER AN ERROR — and this is the one
// place this package chooses that over refusing. What is lost by re-registering
// is nothing a person can see; what is lost by failing is their connection.
func (s *mcpStore) load() map[string]mcpRegistration {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]mcpRegistration{}
	}
	entries := make(map[string]mcpRegistration)
	if err := json.Unmarshal(raw, &entries); err != nil {
		return map[string]mcpRegistration{}
	}
	return entries
}

// get reads one service's registration, reporting separately whether there was
// a usable one.
func (s *mcpStore) get(id string) (mcpRegistration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found, ok := s.load()[id]
	return found, ok && found.usable()
}

// put replaces one service's registration, preserving everything else in the
// file.
func (s *mcpStore) put(id string, record mcpRegistration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries := s.load()
	entries[id] = record
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	temporary, err := os.CreateTemp(directory, ".toolservers-*.json")
	if err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	removeTemporary = false
	return nil
}
