//go:build relayurl

package relayconf

import (
	"crypto/ed25519"
	"flag"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

var relayURL = flag.String("relay-url", "", "base URL of the live relay under test")

// baseURL is the relay under test; a run without one is a failure, never a
// silent pass, because this suite is the only proof a relay works.
func baseURL(t *testing.T) string {
	t.Helper()
	if *relayURL == "" {
		t.Fatal("-relay-url is required: the base URL of the relay to test")
	}
	return strings.TrimRight(*relayURL, "/")
}

// signer is one real device under one real identity, made in a temp home.
type signer struct {
	id  identity.Identity
	dev identity.Dev
}

func (s signer) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s signer) Cert() identity.Cert            { return s.dev.Cert }
func (s signer) Sign(msg []byte) []byte         { return s.dev.Sign(msg) }

// account is one identity and the devices made under it so far. Devices are
// made when first named, so a case that needs fifty of them gets fifty.
type account struct {
	t       *testing.T
	id      identity.Identity
	mu      sync.Mutex
	devices map[string]signer
}

// newAccount makes a fresh identity, so its namespace on the relay is empty.
func newAccount(t *testing.T) *account {
	t.Helper()
	id, err := identity.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &account{t: t, id: id, devices: map[string]signer{}}
}

// device answers the signer for a name, making it under this identity once. A
// device is another home holding the same identity, which is how a second
// machine of one person comes to exist.
func (a *account) device(name string) signer {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s, ok := a.devices[name]; ok {
		return s
	}
	home := a.t.TempDir()
	if err := identity.Save(home, a.id); err != nil {
		a.t.Error(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		a.t.Error(err)
	}
	s := signer{a.id, dev}
	a.devices[name] = s
	return s
}

// sign stamps requests as the named device, timed by now.
func (a *account) sign(name string, now func() time.Time) wireauth.Sign {
	return reqsign.SignFor(a.device(name), now)
}

// deviceID is the id the relay knows a named device by.
func (a *account) deviceID(name string) string { return a.device(name).dev.ID() }

// httpClient bounds every call, so a hung relay fails a case instead of the run.
var httpClient = &http.Client{Timeout: 30 * time.Second}
