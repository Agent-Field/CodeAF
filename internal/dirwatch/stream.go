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
	Close()
}

// Frame is what arrived: the directory's version, or only a pong.
type Frame struct {
	Version uint64
	Pong    bool
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
)

// The close codes the server ends a socket with, and the sentinels they mean.
const (
	CloseRevoked = 4401
	CloseRotated = 4410
)

// The pace of the feed. Each number is here once.
const (
	// KeepEvery is how long a quiet socket waits before it pings. The relay's
	// contract says about thirty seconds, which keeps a middlebox that drops
	// idle connections (Cloudflare at 100 s, nginx and AWS balancers at 60 s)
	// from dropping this one with room for two late ticks; a ping is answered
	// by the relay without waking anything, so it costs nothing to send.
	KeepEvery = 30 * time.Second
	// PongWithin is how long a ping may go unanswered before the socket is
	// dead. It is a third of the keepalive, so a dead socket is found in
	// seconds after a wake or a network change, well inside the 75 s the
	// contract allows.
	PongWithin = KeepEvery / 3
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
