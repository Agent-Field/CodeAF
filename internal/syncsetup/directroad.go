package syncsetup

import (
	"context"
	"io"
	"sync"

	"github.com/Agent-Field/codeaf/internal/livedoor"
)

// THE DIRECT ROAD REACHES THIS PACKAGE THROUGH ONE SEAM, AND THE PROGRAM FILLS IT.
// The road is a WebRTC data channel, and its implementation pulls a peer-to-peer
// library into whatever imports it. Every package that links syncsetup would pay
// for that, tests and the small binaries included, so this package states what it
// needs from the road and the program's entry point installs the real one, the
// same way the live door is handed its engine host. A build or a test that never
// installs it has no direct road: the door advertises none and takes no offer.

// DirectRoadHost is the host half of the direct road for one files door: it
// answers offers (livedoor.DirectHost), is told where an opened stream goes, and
// ends every attempt and stream it holds when the door's context ends.
type DirectRoadHost interface {
	livedoor.DirectHost
	// SetAcceptor installs where an opened stream goes. ctx is the lifetime of
	// the files door's Serve call.
	SetAcceptor(ctx context.Context, accept func(ctx context.Context, stream io.ReadWriteCloser))
	// Close releases everything the host holds. It is safe to call twice.
	Close()
}

// DirectRoad is what the program installs: the host half and the viewer half,
// each given the STUN servers the person's configuration names (an empty list is
// "no STUN request, ever").
type DirectRoad struct {
	// NewHost builds one host half. It must not touch the network.
	NewHost func(stun []string) DirectRoadHost
	// Offer is the viewer half: it signals through s, reads the host's replies
	// from deliver, and returns the opened byte stream or an error.
	Offer func(ctx context.Context, stun []string, s livedoor.Signaller, deliver <-chan []byte) (io.ReadWriteCloser, error)
}

var (
	directRoadMu sync.RWMutex
	directRoad   DirectRoad
)

// SetDirectRoad installs the direct road. It is meant to be called once, from
// the program's entry point, before any door starts.
func SetDirectRoad(d DirectRoad) {
	directRoadMu.Lock()
	directRoad = d
	directRoadMu.Unlock()
}

// InstalledDirectRoad returns the direct road the program installed and whether
// both halves are in place.
func InstalledDirectRoad() (DirectRoad, bool) {
	directRoadMu.RLock()
	d := directRoad
	directRoadMu.RUnlock()
	return d, d.NewHost != nil && d.Offer != nil
}

// DirectRoadInstalled says whether the program installed the direct road, so a
// test of the program's entry point can pin that it did: without it no computer
// ever offers or answers a direct connection, and nothing else would say so.
func DirectRoadInstalled() bool {
	_, ok := InstalledDirectRoad()
	return ok
}
