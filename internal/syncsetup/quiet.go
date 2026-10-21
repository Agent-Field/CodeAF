package syncsetup

// A MACHINE THAT HAS NOT ASKED FOR SYNC IS QUIET. The first launch makes an
// identity (a fleet of one) and nothing else: no request, no socket, no device
// record reaches the sync service until the person pairs, opens the
// add-machine card, or already has a fleet. The identity says so itself (identity.Solo); whatever
// starts a fleet ends it.
//
// ONE GATE, AT THE WIRE. Every client built here sends through [quietWire],
// which refuses while the marker stands, so no caller needs a special case.
// The two reads a screen makes (the device list and the chat list) read an
// empty directory while quiet, which is what a fleet of one looks like.

import (
	"context"
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// ErrQuiet is what the wire says while this machine has not asked for sync.
var ErrQuiet = errors.New("sync is waiting until another machine is added")

// MayTalk is the one question every path asks before it reaches the sync
// service: the machine has an identity, and that identity is not solo.
func MayTalk(home string) bool {
	_, err := identity.Load(home)
	return err == nil && !identity.Solo(home)
}

// Quiet says this machine may not talk to the sync service yet.
func Quiet(home string) bool { return !MayTalk(home) }

// quietWire is the one gate: nothing leaves the machine while it is quiet.
type quietWire struct {
	home string
	next http.RoundTripper
}

func (w quietWire) RoundTrip(r *http.Request) (*http.Response, error) {
	if Quiet(w.home) {
		return nil, ErrQuiet
	}
	return w.next.RoundTrip(r)
}

func gated(home string, c *http.Client) *http.Client {
	next := c.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	c.Transport = quietWire{home: home, next: next}
	return c
}

// list reads the directory, or an empty one while quiet: no devices, no chats.
func (s *Sync) list(ctx context.Context) (directory.Listing, error) {
	if Quiet(s.Home) {
		return directory.Listing{}, nil
	}
	return s.Dir.List(ctx)
}
