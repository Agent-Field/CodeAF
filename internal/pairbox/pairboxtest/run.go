// Package pairboxtest is the conformance suite every pairbox.Box must pass,
// plus the fake clock it runs on. It is the only definition of "a mailbox
// works": the in-process Memory, the wire in front of it and any hosted relay
// are held to the same cases.
package pairboxtest

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// Rig is one mailbox service as a suite sees it.
type Rig struct {
	// Box is the service under test.
	Box pairbox.Box
	// Clock is the service's own time, which a case waits on to see expiry.
	Clock Clock
	// URL is the base URL of the wire when the service can also be spoken to as
	// raw HTTP. Empty skips the cases that read bytes off the wire.
	URL string
	// Small is a Box over a service capped at two live mailboxes, or nil, which
	// skips RelayFull.
	Small pairbox.Box
}

// Run runs every conformance case against a rig from factory, one rig per case.
//
// Each case speaks as a network of its own, through pairbox.WithPeer, so the
// per-network limits one case spends never reach another. Memory honours that
// directly and so does a wire in front of it built with pairbox.ForwardedPeer.
// A live relay is therefore tested when it was started with --trust-proxy, or
// with limits raised past what the whole suite spends from one network;
// otherwise every case shares the caller's address and the rate cases fail
// because of the budget the earlier ones used, not because of the relay.
//
// Every mailbox a case makes is deleted when it ends, so a live relay is left
// empty.
func Run(t *testing.T, factory func(t *testing.T) Rig) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newEnv(t, c.name, factory(t))) })
	}
}

// cases is the table of the suite, in the order of contract section 18.9. A
// slice, not a map, so a failing run reads the same every time.
var cases = []struct {
	name string
	run  func(*testing.T, env)
}{
	{"Limits", limitsAreTheirWord},
	{"UnknownIs404", unknownIs404},
	{"ExpiryDeletes", expiryDeletes},
	{"DeleteWakesPoll", deleteWakesPoll},
	{"ClaimRace", claimRace},
	{"MessageTooBig", messageTooBig},
	{"SideFull", sideFull},
	{"CreateRateLimited", createRateLimited},
	{"WriteRateLimited", writeRateLimited},
	{"RelayFull", relayFull},
	{"NoListing", noListing},
	{"NoKeyEcho", noKeyEcho},
	{"Opaque", opaque},
	{"NameplatesDistinct", nameplatesDistinct},
	{"WrongKey", wrongKey},
	{"LongPoll", longPoll},
}

// caseBudget bounds a whole case, so a relay that never answers fails the case
// instead of hanging the run.
const caseBudget = 3 * time.Minute

// env is what one case works with: its rig, the numbers the rig advertises and
// a context that names the case's network.
type env struct {
	Rig
	lim  pairbox.Limits
	ctx  context.Context
	peer string
}

func newEnv(t *testing.T, name string, rig Rig) env {
	t.Helper()
	peer := "case-" + name
	ctx, cancel := context.WithTimeout(pairbox.WithPeer(context.Background(), peer), caseBudget)
	t.Cleanup(cancel)
	lim, err := rig.Box.Limits(ctx)
	if err != nil {
		t.Fatalf("reading the mailbox's limits: %v", err)
	}
	return env{Rig: rig, lim: lim, ctx: ctx, peer: peer}
}
