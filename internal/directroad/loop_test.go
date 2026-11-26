package directroad

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/livedoor"
)

// Contract V30: a direct road opens between two computers over loopback and
// carries bytes across message boundaries in both directions.
func TestTwoPeersConnectOverLoopbackAndEchoBytesAcrossMessageBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cfg := loopbackConfig()
	host := New(cfg)
	defer host.Close()

	var acceptedCount atomic.Int32
	hostStreamCh := make(chan io.ReadWriteCloser, 1)
	acceptDone := make(chan struct{})
	defer close(acceptDone)

	host.SetAcceptor(ctx, func(_ context.Context, stream io.ReadWriteCloser) {
		acceptedCount.Add(1)
		hostStreamCh <- stream
		<-acceptDone
	})

	bridge := newSignalBridge(ctx, host, livedoor.Peer{Device: "viewer-loopback"})

	viewerStream, err := Offer(ctx, cfg, bridge.viewerSignaller(), bridge.deliver())
	if err != nil {
		t.Fatalf("Offer failed: %v", err)
	}
	defer func() { _ = viewerStream.Close() }()

	var hostStream io.ReadWriteCloser
	select {
	case hostStream = <-hostStreamCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for host acceptor to receive stream")
	}

	if got := acceptedCount.Load(); got != 1 {
		t.Fatalf("host acceptor received %d streams, want 1", got)
	}

	// Build the three writes: 1 intent byte, 2-byte prefix + body, and 100 KiB.
	intent := []byte{'D'}
	recordBody := bytes.Repeat([]byte("handshake-and-record-"), 60)
	prefixed := make([]byte, 2+len(recordBody))
	binary.BigEndian.PutUint16(prefixed[:2], uint16(len(recordBody)))
	copy(prefixed[2:], recordBody)

	large := make([]byte, 100<<10)
	for i := range large {
		large[i] = byte((i*29 + 11) & 0xff)
	}

	hostErrCh := make(chan error, 1)
	go func() {
		totalLen := len(intent) + len(prefixed) + len(large)
		buf := make([]byte, totalLen)
		if _, err := io.ReadFull(hostStream, buf); err != nil {
			hostErrCh <- err
			return
		}
		if _, err := hostStream.Write(buf); err != nil {
			hostErrCh <- err
			return
		}
		hostErrCh <- nil
	}()

	if _, err := viewerStream.Write(intent); err != nil {
		t.Fatalf("viewer Write(1 byte): %v", err)
	}
	if _, err := viewerStream.Write(prefixed); err != nil {
		t.Fatalf("viewer Write(prefixed): %v", err)
	}
	if _, err := viewerStream.Write(large); err != nil {
		t.Fatalf("viewer Write(100 KiB): %v", err)
	}

	var gotIntent [1]byte
	if _, err := io.ReadFull(viewerStream, gotIntent[:]); err != nil {
		t.Fatalf("viewer ReadFull(1 byte): %v", err)
	}
	if gotIntent[0] != intent[0] {
		t.Fatalf("echoed intent byte = %q, want %q", gotIntent[0], intent[0])
	}

	var gotHdr [2]byte
	if _, err := io.ReadFull(viewerStream, gotHdr[:]); err != nil {
		t.Fatalf("viewer ReadFull(2-byte prefix): %v", err)
	}
	gotBodyLen := int(binary.BigEndian.Uint16(gotHdr[:]))
	if gotBodyLen != len(recordBody) {
		t.Fatalf("echoed prefix length = %d, want %d", gotBodyLen, len(recordBody))
	}
	gotBody := make([]byte, gotBodyLen)
	if _, err := io.ReadFull(viewerStream, gotBody); err != nil {
		t.Fatalf("viewer ReadFull(body): %v", err)
	}
	if !bytes.Equal(gotBody, recordBody) {
		t.Fatal("echoed record body mismatch")
	}

	gotLarge := make([]byte, len(large))
	if _, err := io.ReadFull(viewerStream, gotLarge); err != nil {
		t.Fatalf("viewer ReadFull(100 KiB): %v", err)
	}
	if !bytes.Equal(gotLarge, large) {
		t.Fatal("echoed 100 KiB payload mismatch")
	}

	if err := <-hostErrCh; err != nil {
		t.Fatalf("host echo goroutine failed: %v", err)
	}

	// Closing the viewer's stream must end the host's next read with an
	// error/EOF.
	if err := viewerStream.Close(); err != nil {
		t.Fatalf("viewerStream.Close: %v", err)
	}

	closeReadCh := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := hostStream.Read(b[:])
		closeReadCh <- err
	}()

	select {
	case err := <-closeReadCh:
		if err == nil {
			t.Fatal("expected host Read after viewer Close to return an error/EOF, got nil")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for host Read to end after viewer Close")
	}
}
