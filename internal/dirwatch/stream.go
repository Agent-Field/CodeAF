package dirwatch

import (
	"context"
	"errors"
	"time"
)

// Stream is one open socket, as the feed needs it.
type Stream interface {
	// Next waits for the next sign of life: a version frame, or the answer to a
	// ping. A refusal the server closed the socket with comes back as the
	// sentinel it names.
	Next(ctx context.Context) (Frame, error)
	// Ping asks the server to answer, which is how a quiet socket is told from
	// a dead one.
	Ping(ctx context.Context) error
	// Vouching says whether the server answered the upgrade with the word that
	// it counts this socket as proof of life for the leases it names. A server
	// that never says so is an old one, and its socket proves nothing.
	Vouching() bool
	Close()
}

// Frame is what arrived: the directory's version, an event, or only a pong.
type Frame struct {
	Version uint64
	Pong    bool
	// Event is set for an event frame (it has no version); nil otherwise.
	Event *Event
}

// Dialer opens one socket.
type Dialer func(ctx context.Context) (Stream, error)

// The ways a socket ends that are not "try again".
var (
	// ErrNoRoute is a relay that has no watch route: an old or a self-hosted one.
	ErrNoRoute = errors.New("dirwatch: this relay has no watch route")
	// ErrRevoked and ErrRotated are the server saying the device is revoked or
	// the identity replaced. Reconnecting would only be refused again; the list
	// read says it in the words every surface already uses.
	ErrRevoked = errors.New("dirwatch: device revoked")
	ErrRotated = errors.New("dirwatch: identity rotated")
	// ErrSilent is the relay saying it stopped hearing this device and closed
	// the socket. It is not a refusal: the device is welcome, so the feed dials
	// again at once.
	ErrSilent = errors.New("dirwatch: the relay stopped hearing this socket")
)

// The close codes the server ends a socket with, and the sentinels they mean.
const (
	CloseRevoked = 4401
	CloseRotated = 4410
	// CloseSilent ends a socket whose last sign of life was too old; the
	// device is announced offline and must redial (STAGE-1-CONTRACTS.md 21.12.4).
	CloseSilent = 4408
)

// The pace of the feed. Each number is here once.
const (
	// KeepEvery is how often a quiet socket pings: every 10 s, the beat
	// STAGE-1-CONTRACTS.md 21.12 gives. The relay derives the window in which
	// it counts this device online from the beat the dial declares, and the
	// directory package derives that declaration from this constant, so the
	// two halves cannot drift. The relay answers a ping without waking
	// anything, so it costs nothing, and it keeps a middlebox that drops idle
	// connections from dropping this one.
	KeepEvery = 10 * time.Second
	// DeadAfter is how long a socket may stay silent before it is dead: 2.5
	// beats, 25 s. That tolerates one lost pong and a late second one, the
	// same factor the relay uses for its presence window.
	DeadAfter = KeepEvery * 5 / 2
	// PongWithin is how long a ping may go unanswered: what is left of DeadAfter
	// once the quiet wait before the ping has passed.
	PongWithin = DeadAfter - KeepEvery
	// RetryBase and RetryCap bound the full-jitter wait between reconnects: the
	// ceiling doubles from the base up to the cap, and the wait is anywhere
	// below the ceiling, so many devices that lost the relay together do not
	// return together.
	RetryBase = time.Second
	RetryCap  = time.Minute
	// ProbeEvery is how long a relay without the route is left alone before it
	// is asked once more, because it may have been upgraded.
	ProbeEvery = time.Hour
)
