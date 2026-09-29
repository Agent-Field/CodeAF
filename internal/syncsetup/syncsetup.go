// Package syncsetup is the one place that turns the person's settings and
// identity into the real clients of a relay. Everything else takes a Sync, or
// nothing: with no relay set there is no Sync and the program behaves exactly
// as it did before sync existed.
//
// It is also, with the relay itself, the only place that binds the real
// request signing (reqsign) to the two wires, so the wires never import it.
package syncsetup

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

const (
	// URLVar names the relay. Unset means sync is off.
	URLVar = "CODEAF_SYNC_URL"
	// IntervalVar is how often unsaved turns are flushed, in milliseconds.
	IntervalVar = "CODEAF_SYNC_INTERVAL_MS"
	// DefaultInterval is the flush interval when IntervalVar is unset.
	DefaultInterval = 5 * time.Second
	// requestTimeout bounds one request so a silent relay cannot hang a caller.
	requestTimeout = 30 * time.Second
)

// ErrNoIdentity is what a machine says when a relay is set but it has no
// identity to sign with. Its words are the list's own, so every surface agrees.
var ErrNoIdentity = errors.New(chatlist.NoIdentity)

// Sync is everything that talks to the relay, bound to this device.
type Sync struct {
	Dir      directory.Client    // bound to this device by its signer
	Store    blobstore.Store     // the store wire, counted
	Counters *blobstore.Counters // what Store has been asked to move
	Device   identity.Dev
	Identity identity.Identity
	Ledger   string        // names the published ledger for this relay and identity
	Interval time.Duration // flush interval
}

// Open builds the clients for the relay named by CODEAF_SYNC_URL. It answers
// ok = false with no error when no relay is set, and touches nothing on disk
// in that case. A relay with no identity here, or a URL that is not a web
// address, is an error of one sentence.
func Open(home string) (*Sync, bool, error) {
	raw := env.Get(URLVar)
	if raw == "" {
		return nil, false, nil
	}
	base, err := relayBase(raw)
	if err != nil {
		return nil, false, err
	}
	id, err := loadIdentity(home)
	if err != nil {
		return nil, false, err
	}
	dev, err := identity.Device(home)
	if err != nil {
		return nil, false, err
	}
	interval, err := flushInterval()
	if err != nil {
		return nil, false, err
	}
	return build(base, id, dev, interval), true, nil
}

// relayBase checks that s is an http or https address with a host, and returns
// it without a trailing slash.
func relayBase(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%s is not a web address like http://host:8787", URLVar)
	}
	return u.Scheme + "://" + u.Host + u.Path, nil
}

// loadIdentity reads the identity without making one: a machine that has none
// must be told so, not quietly given a fresh identity nobody else knows.
func loadIdentity(home string) (identity.Identity, error) {
	id, err := identity.Load(home)
	if errors.Is(err, identity.ErrNone) {
		return id, ErrNoIdentity
	}
	return id, err
}

// flushInterval reads the interval, which must be a positive number of
// milliseconds when it is set.
func flushInterval() (time.Duration, error) {
	raw := env.Get(IntervalVar)
	if raw == "" {
		return DefaultInterval, nil
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return 0, fmt.Errorf("%s is not a positive number of milliseconds", IntervalVar)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// build binds both wires to one signer and one client, and counts the store.
func build(base string, id identity.Identity, dev identity.Dev, interval time.Duration) *Sync {
	sign := reqsign.SignFor(deviceSigner{id, dev}, time.Now)
	hc := &http.Client{Timeout: requestTimeout}
	counters := &blobstore.Counters{}
	return &Sync{
		Dir:      directory.NewHTTP(base, sign, hc),
		Store:    blobstore.Counting{Inner: blobstore.NewHTTP(base, sign, hc), C: counters},
		Counters: counters,
		Device:   dev,
		Identity: id,
		Ledger:   ledgerName(base, id.ID()),
		Interval: interval,
	}
}

// deviceSigner is the reqsign.Signer of this machine: the device key signs,
// and the identity's public key is what verifies the device's cert.
type deviceSigner struct {
	id  identity.Identity
	dev identity.Dev
}

func (s deviceSigner) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s deviceSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s deviceSigner) Sign(msg []byte) []byte         { return s.dev.Sign(msg) }

// ledgerName names the published ledger of one (relay, identity) pair, so that
// pointing at a new or wiped relay starts an empty ledger. It is the same
// formula as cellstore.LedgerName (contract §5), which this package will call
// once that lands.
func ledgerName(relay, identityID string) string {
	sum := sha256.Sum256([]byte(relay + "\n" + identityID))
	return hex.EncodeToString(sum[:8])
}

// Rows makes a Sync the chat list's source: every chat the person has on any
// machine, with names opened under the metadata key of their cell key.
func (s *Sync) Rows(ctx context.Context) ([]chatlist.Row, error) {
	l, err := s.Dir.List(ctx)
	if err != nil {
		return nil, err
	}
	key := directory.MetadataKey(s.Identity.CellKey())
	open := func(sealed string) (string, error) { return directory.OpenName(key, sealed) }
	return chatlist.Rows(l, s.Device.ID(), open), nil
}

var _ chatlist.Source = (*Sync)(nil)
