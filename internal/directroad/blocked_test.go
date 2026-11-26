package directroad

import (
	"context"
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/livedoor"
)

// Contract V32: a blocked path never opens, Offer ends within its context
// bound, the host releases its pending slot, and no goroutines or peer
// connections leak.
func TestABlockedPathNeverOpensAndTheAttemptEndsWithinItsBound(t *testing.T) {
	rig := newVNetRig(t)
	rig.dropPeers.Store(true)

	baseGoroutines := runtime.NumGoroutine()

	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()

	host := New(rig.hostConfig())
	defer host.Close()

	var accepted atomic.Int32
	host.SetAcceptor(hostCtx, func(_ context.Context, _ io.ReadWriteCloser) {
		accepted.Add(1)
	})

	const attemptBound = 900 * time.Millisecond
	offerCtx, offerCancel := context.WithTimeout(context.Background(), attemptBound)
	defer offerCancel()

	peer := livedoor.Peer{Device: "device-blocked-v32"}
	bridge := newSignalBridge(offerCtx, host, peer)

	start := time.Now()
	stream, err := Offer(offerCtx, rig.viewerConfig(), bridge.viewerSignaller(), bridge.deliver())
	elapsed := time.Since(start)

	if err == nil {
		_ = stream.Close()
		t.Fatal("Offer succeeded across a completely blocked vnet path")
	}
	if stream != nil {
		t.Fatalf("Offer returned non-nil stream on error: %v", stream)
	}
	if elapsed > attemptBound+1500*time.Millisecond {
		t.Fatalf("Offer took %v, exceeding context bound %v", elapsed, attemptBound)
	}
	if got := accepted.Load(); got != 0 {
		t.Fatalf("host acceptor called %d times on blocked path, want 0", got)
	}

	// The end frame sent by Offer (or superseding) releases the host's pending
	// slot so a second offer from the same device is accepted.
	eventually(t, 2*time.Second, "host pending slot release after blocked offer", func() bool {
		return host.pendingCount() == 0
	})

	secondOfferPayload, encErr := livedoor.EncodeSignal(livedoor.Signal{
		T:   livedoor.SignalOffer,
		ID:  "0123456789abcdef",
		SDP: "v=0\r\n",
	})
	if encErr != nil {
		t.Fatalf("EncodeSignal: %v", encErr)
	}
	secondSess := host.Offered(hostCtx, peer, secondOfferPayload, fnSignaller(func([]byte) error { return nil }))
	if secondSess == nil {
		t.Fatal("host rejected second offer from the same device after first attempt ended")
	}
	secondSess.Close()

	host.Close()
	eventually(t, 3*time.Second, "goroutines to return to baseline after host close", func() bool {
		return runtime.NumGoroutine() <= baseGoroutines+4
	})
}

// Contract V33: a path that dies after opening closes the stream on BOTH ends
// within Disconnected + Failed plus a reasonable margin.
func TestABlockedPathAfterOpenClosesTheStreamWithinTheFailedTimeout(t *testing.T) {
	rig := newVNetRig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	host := New(rig.hostConfig())
	defer host.Close()

	hostStreamCh := make(chan io.ReadWriteCloser, 1)
	acceptDone := make(chan struct{})
	defer close(acceptDone)

	host.SetAcceptor(ctx, func(_ context.Context, stream io.ReadWriteCloser) {
		hostStreamCh <- stream
		<-acceptDone
	})

	bridge := newSignalBridge(ctx, host, livedoor.Peer{Device: "device-cut-v33"})
	viewerStream, err := Offer(ctx, rig.viewerConfig(), bridge.viewerSignaller(), bridge.deliver())
	if err != nil {
		t.Fatalf("Offer over vnet failed: %v", err)
	}
	defer func() { _ = viewerStream.Close() }()

	var hostStream io.ReadWriteCloser
	select {
	case hostStream = <-hostStreamCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for host stream over vnet")
	}

	// Exchange one byte to verify the data channel is live before cutting the
	// network.
	if _, err := viewerStream.Write([]byte{'D'}); err != nil {
		t.Fatalf("initial viewer Write: %v", err)
	}
	var one [1]byte
	if _, err := io.ReadFull(hostStream, one[:]); err != nil {
		t.Fatalf("initial host ReadFull: %v", err)
	}

	// Cut all UDP packets between the peers and time how long both blocked
	// reads take to fail.
	rig.dropPeers.Store(true)
	cutAt := time.Now()

	viewerErrCh := make(chan error, 1)
	hostErrCh := make(chan error, 1)

	go func() {
		var b [1]byte
		_, rerr := viewerStream.Read(b[:])
		viewerErrCh <- rerr
	}()
	go func() {
		var b [1]byte
		_, rerr := hostStream.Read(b[:])
		hostErrCh <- rerr
	}()

	const maxFailWait = 3500 * time.Millisecond

	select {
	case verr := <-viewerErrCh:
		if verr == nil {
			t.Fatal("viewer Read returned nil error after path died")
		}
	case <-time.After(maxFailWait):
		t.Fatalf("viewer Read did not fail within %v after path died", maxFailWait)
	}

	select {
	case herr := <-hostErrCh:
		if herr == nil {
			t.Fatal("host Read returned nil error after path died")
		}
	case <-time.After(maxFailWait):
		t.Fatalf("host Read did not fail within %v after path died", maxFailWait)
	}

	if elapsed := time.Since(cutAt); elapsed > maxFailWait {
		t.Fatalf("streams closed in %v, want <= %v", elapsed, maxFailWait)
	}
}

// Contract security: when a viewer vanishes without closing its stream, the
// host's ICE agent reaches Failed and closes the host's stream so the acceptor
// goroutine exits promptly.
func TestAHostDropsADirectTunnelWhenTheViewerVanishes(t *testing.T) {
	rig := newVNetRig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	host := New(rig.hostConfig())
	defer host.Close()

	acceptorEntered := make(chan struct{})
	acceptorExited := make(chan time.Duration, 1)
	var hostStreamRef atomic.Pointer[byteStream]

	host.SetAcceptor(ctx, func(_ context.Context, stream io.ReadWriteCloser) {
		if bs, ok := stream.(*byteStream); ok {
			hostStreamRef.Store(bs)
		}
		close(acceptorEntered)
		var buf [64]byte
		for {
			if _, err := stream.Read(buf[:]); err != nil {
				return
			}
		}
	})

	bridge := newSignalBridge(ctx, host, livedoor.Peer{Device: "device-vanish"})
	viewerStream, err := Offer(ctx, rig.viewerConfig(), bridge.viewerSignaller(), bridge.deliver())
	if err != nil {
		t.Fatalf("Offer over vnet failed: %v", err)
	}
	defer func() { _ = viewerStream.Close() }()

	select {
	case <-acceptorEntered:
	case <-ctx.Done():
		t.Fatal("timed out waiting for host acceptor to start")
	}

	// The viewer's network disappears without viewerStream.Close() being
	// called.
	vanishedAt := time.Now()
	rig.dropPeers.Store(true)

	go func() {
		eventually(t, 3500*time.Millisecond, "host stream to close after viewer vanishes", func() bool {
			bs := hostStreamRef.Load()
			return bs != nil && bs.closed.Load() && host.streamCount() == 0
		})
		acceptorExited <- time.Since(vanishedAt)
	}()

	select {
	case elapsed := <-acceptorExited:
		if elapsed > 3500*time.Millisecond {
			t.Fatalf("host took %v to drop vanished viewer tunnel, want <= 3.5s", elapsed)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for host acceptor goroutine to exit after viewer vanished")
	}
}
