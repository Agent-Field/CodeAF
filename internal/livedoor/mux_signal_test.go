package livedoor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// signalInbox collects, in order, every signalling payload a mux hands to its
// OnSignal callback.
type signalInbox struct {
	got chan []byte
}

func newSignalInbox() *signalInbox { return &signalInbox{got: make(chan []byte, 256)} }

func (s *signalInbox) take(t *testing.T) []byte {
	t.Helper()
	select {
	case p := <-s.got:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("no signalling payload arrived")
		return nil
	}
}

func (s *signalInbox) onSignal(p []byte) { s.got <- append([]byte(nil), p...) }

// V37: OpSignal rides channel 0 of an open link, a mux that does not know an op
// still skips it (so an older build ignores signalling), and a signal on any
// other channel is a protocol error that ends the link.
func TestAnUnknownOpIsStillSkippedAndSignalRidesChannelZero(t *testing.T) {
	t.Run("the codec round-trips a signal and OpSignal is the only new known op", func(t *testing.T) {
		var buf bytes.Buffer
		want := frame{op: OpSignal, channel: 0, payload: []byte(`{"v":1,"t":"offer","id":"abc"}`)}
		if _, err := writeFrame(&buf, want, ChunkBytes); err != nil {
			t.Fatal(err)
		}
		got, err := readFrame(&buf, ChunkBytes, nil)
		if err != nil || got.op != OpSignal || got.channel != 0 || !bytes.Equal(got.payload, want.payload) {
			t.Fatalf("round trip = %+v, %v; want %+v", got, err, want)
		}
		if !isKnownOp(OpSignal) || isKnownOp(OpSignal+1) || isKnownOp(0) {
			t.Fatal("isKnownOp does not stop exactly at OpSignal")
		}
		if OpSignal != 11 {
			t.Fatalf("OpSignal = %d; the wire number is 11", OpSignal)
		}
	})

	t.Run("an op the reader does not know is consumed and the next frame decodes", func(t *testing.T) {
		var buf bytes.Buffer
		payload := []byte("a future extension")
		var hdr [frameHeaderBytes]byte
		hdr[0] = OpSignal + 1
		binary.BigEndian.PutUint32(hdr[5:9], uint32(len(payload)))
		buf.Write(hdr[:])
		buf.Write(payload)
		next := frame{op: OpPing, channel: 0, payload: encodeUint32(7)}
		if _, err := writeFrame(&buf, next, ChunkBytes); err != nil {
			t.Fatal(err)
		}
		got, err := readFrame(&buf, ChunkBytes, nil)
		if err != nil || got.op != OpPing {
			t.Fatalf("the frame after an unknown op = %+v, %v; want the ping", got, err)
		}
	})

	t.Run("signals arrive in order in both directions", func(t *testing.T) {
		viewerInbox, hostInbox := newSignalInbox(), newSignalInbox()
		v, h := newMuxPair(t, MuxConfig{OnSignal: viewerInbox.onSignal}, MuxConfig{OnSignal: hostInbox.onSignal})
		for i := 0; i < 5; i++ {
			if err := v.SendSignal([]byte(fmt.Sprintf("to-host-%d", i))); err != nil {
				t.Fatal(err)
			}
			if err := h.SendSignal([]byte(fmt.Sprintf("to-viewer-%d", i))); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 5; i++ {
			if got := hostInbox.take(t); string(got) != fmt.Sprintf("to-host-%d", i) {
				t.Fatalf("host's signal %d = %q", i, got)
			}
			if got := viewerInbox.take(t); string(got) != fmt.Sprintf("to-viewer-%d", i) {
				t.Fatalf("viewer's signal %d = %q", i, got)
			}
		}
	})

	t.Run("a signal on a channel other than zero ends the link", func(t *testing.T) {
		near, far := net.Pipe()
		t.Cleanup(func() { _ = near.Close(); _ = far.Close() })
		host := NewMux(far, Host, MuxConfig{})
		t.Cleanup(func() { _ = host.Close() })
		go func() {
			_, _ = writeFrame(near, frame{op: OpSignal, channel: 3, payload: []byte("{}")}, ChunkBytes)
		}()
		select {
		case <-host.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("a signal on channel 3 did not end the link")
		}
	})
}

// V37: a signalling payload over SignalMax is refused by the sender and ends the
// link at the receiver, and more than SignalRate frames in a minute is refused by
// the sender and ends the link at the receiver (`too-many`).
func TestASignalFrameOverTheLimitIsRefused(t *testing.T) {
	t.Run("the sender refuses a payload over the limit and sends nothing", func(t *testing.T) {
		inbox := newSignalInbox()
		v, _ := newMuxPair(t, MuxConfig{}, MuxConfig{OnSignal: inbox.onSignal})
		if err := v.SendSignal(make([]byte, SignalMax+1)); !errors.Is(err, ErrSignalTooLarge) {
			t.Fatalf("SendSignal(SignalMax+1) = %v, want ErrSignalTooLarge", err)
		}
		if err := v.SendSignal(make([]byte, SignalMax)); err != nil {
			t.Fatalf("SendSignal(SignalMax) = %v, want it sent", err)
		}
		if got := inbox.take(t); len(got) != SignalMax {
			t.Fatalf("the host got %d bytes, want exactly the one payload at the limit", len(got))
		}
		select {
		case extra := <-inbox.got:
			t.Fatalf("a refused payload still reached the host (%d bytes)", len(extra))
		default:
		}
	})

	t.Run("a receiver ends the link on a payload over the limit", func(t *testing.T) {
		near, far := net.Pipe()
		t.Cleanup(func() { _ = near.Close(); _ = far.Close() })
		host := NewMux(far, Host, MuxConfig{})
		t.Cleanup(func() { _ = host.Close() })
		// ChunkBytes is larger than SignalMax, so the frame itself is well formed.
		go func() {
			_, _ = writeFrame(near, frame{op: OpSignal, channel: 0, payload: make([]byte, SignalMax+1)}, ChunkBytes)
		}()
		select {
		case <-host.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("an oversize signal did not end the link")
		}
	})

	t.Run("the sender stops at SignalRate frames a minute and starts again after it", func(t *testing.T) {
		fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
		inbox := newSignalInbox()
		v, _ := newMuxPair(t, MuxConfig{Clock: fc}, MuxConfig{Clock: fc, OnSignal: inbox.onSignal})
		for i := 0; i < SignalRate; i++ {
			if err := v.SendSignal([]byte("x")); err != nil {
				t.Fatalf("frame %d of %d was refused: %v", i+1, SignalRate, err)
			}
			inbox.take(t)
		}
		if err := v.SendSignal([]byte("x")); !errors.Is(err, ErrTooManySignals) {
			t.Fatalf("frame %d in a minute = %v, want ErrTooManySignals", SignalRate+1, err)
		}
		fc.Advance(time.Minute + time.Second)
		if err := v.SendSignal([]byte("x")); err != nil {
			t.Fatalf("a frame a minute later was refused: %v", err)
		}
		inbox.take(t)
	})

	t.Run("a receiver ends the link on more than SignalRate frames in a minute", func(t *testing.T) {
		fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
		near, far := net.Pipe()
		t.Cleanup(func() { _ = near.Close(); _ = far.Close() })
		inbox := newSignalInbox()
		host := NewMux(far, Host, MuxConfig{Clock: fc, OnSignal: inbox.onSignal})
		t.Cleanup(func() { _ = host.Close() })
		go func() {
			for i := 0; i <= SignalRate; i++ {
				if _, err := writeFrame(near, frame{op: OpSignal, channel: 0, payload: []byte("x")}, ChunkBytes); err != nil {
					return
				}
			}
		}()
		select {
		case <-host.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("a flood of signals did not end the link")
		}
		if n := len(inbox.got); n != SignalRate {
			t.Fatalf("the host handed %d signals to its handler, want exactly %d before ending the link", n, SignalRate)
		}
	})
}
