//go:build relayurl

package relayconf

import (
	"flag"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/pairbox/pairboxtest"
)

// smallURL is a second relay capped at two mailboxes, the service RelayFull needs; without one
// that case is skipped.
var smallURL = flag.String("relay-small-url", "", "base URL of a relay capped at two live mailboxes, for RelayFull")

// A separate expiry service keeps the live-clock expiry assertion short without
// expiring an authorization case's mailbox while the local proxy is busy.
var expiryURL = flag.String("relay-expiry-url", "", "base URL of a relay with a mailbox lifetime below five seconds, for ExpiryDeletes")

// TestPairingConformance holds a live relay's pairing mailbox to the suite that
// defines one. The relay must be started with --trust-proxy, or with limits
// raised past what the suite spends, so each case can be a network of its own.
// A relay without a small service leaves RelayFull skipped, and one whose TTL
// is over five seconds leaves ExpiryDeletes skipped.
func TestPairingConformance(t *testing.T) {
	base := baseURL(t)
	pairboxtest.Run(t, func(t *testing.T) pairboxtest.Rig {
		service := base
		if *expiryURL != "" && strings.HasSuffix(t.Name(), "/ExpiryDeletes") {
			service = *expiryURL
		}
		rig := pairboxtest.Rig{
			Box:   pairbox.NewHTTP(service, httpClient),
			Clock: relayClock{t, service},
			URL:   service,
		}
		if *smallURL != "" {
			rig.Small = pairbox.NewHTTP(*smallURL, httpClient)
		}
		return rig
	})
}
