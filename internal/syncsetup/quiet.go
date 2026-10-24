package syncsetup

// A MACHINE THAT HAS NOT ASKED FOR SYNC IS QUIET. The first launch makes an
// identity (a fleet of one) and nothing else: no request, no socket, no device
// record reaches the sync service until the person pairs, offers a pairing
// code, or already has a fleet. The identity says so itself (identity.Solo); whatever
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
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// ErrQuiet is what the wire says while this machine has not asked for sync.
var ErrQuiet = errors.New("sync is waiting until another machine is added")

// MayTalk is the one question every path asks before it reaches the sync
// service: sync is on, the machine has an identity, and that identity is not solo.
func MayTalk(home string) bool {
	if !Resolve(home).On() {
		return false
	}
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

// ClusterHTTP supplies the same use-time quiet gate for enrollment, discovery,
// exchange and socket upgrades. Constructing it performs no network operation;
// handles created before pairing or sync-off recheck the decision on every use.
func (s *Sync) ClusterHTTP(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	return gated(s.Home, &copyClient)
}

// quietStream rechecks an existing socket at every read/ping, so a later off
// decision cannot retain a second transport path around quietWire.
type quietStream struct {
	dirwatch.Stream
	home string
}

func (s quietStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	if Quiet(s.home) {
		s.Stream.Close()
		return dirwatch.Frame{}, ErrQuiet
	}
	return s.Stream.Next(ctx)
}
func (s quietStream) Ping(ctx context.Context) error {
	if Quiet(s.home) {
		s.Stream.Close()
		return ErrQuiet
	}
	return s.Stream.Ping(ctx)
}
func (s quietStream) Vouching() bool { return !Quiet(s.home) && s.Stream.Vouching() }
