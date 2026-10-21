package pairbox_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/pairbox/pairboxtest"
)

// smallLimits is a service that holds two mailboxes, for RelayFull.
func smallLimits() pairbox.Limits {
	l := pairbox.DefaultLimits
	l.MaxBoxes = 2
	return l
}

func TestMemoryConformance(t *testing.T) {
	pairboxtest.Run(t, func(t *testing.T) pairboxtest.Rig {
		clock := pairboxtest.NewFakeClock()
		return pairboxtest.Rig{
			Box:   pairbox.NewMemory(pairbox.DefaultLimits, clock.Now),
			Clock: clock,
			Small: pairbox.NewMemory(smallLimits(), clock.Now),
		}
	})
}

// The same mailbox behind the wire, so the handler and the client are held to
// the suite too, and the raw-HTTP cases have something to read.
func TestWireConformance(t *testing.T) {
	pairboxtest.Run(t, func(t *testing.T) pairboxtest.Rig {
		clock := pairboxtest.NewFakeClock()
		serve := func(l pairbox.Limits) (string, pairbox.Box) {
			srv := httptest.NewServer(pairbox.Handler(pairbox.NewMemory(l, clock.Now), pairbox.ForwardedPeer))
			t.Cleanup(srv.Close)
			return srv.URL, pairbox.NewHTTP(srv.URL, srv.Client())
		}
		url, box := serve(pairbox.DefaultLimits)
		_, small := serve(smallLimits())
		return pairboxtest.Rig{Box: box, Clock: clock, URL: url, Small: small}
	})
}

// A relay from before pairing answers 404 on the limits, which the client
// reports as its own error so a device can say the relay is too old.
func TestHTTPReportsATooOldRelay(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := pairbox.NewHTTP(srv.URL, srv.Client()).Limits(t.Context())
	if err != pairbox.ErrTooOld {
		t.Fatalf("Limits against a relay without pairing = %v, want ErrTooOld", err)
	}
}
