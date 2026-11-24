package livedoor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

func newMuxPair(t *testing.T, viewerCfg, hostCfg MuxConfig) (*Mux, *Mux) {
	t.Helper()
	c1, c2 := net.Pipe()
	v := NewMux(c1, Viewer, viewerCfg)
	h := NewMux(c2, Host, hostCfg)
	t.Cleanup(func() {
		_ = v.Close()
		_ = h.Close()
	})
	return v, h
}

func openAcceptedPair(t *testing.T, v, h *Mux, chat string) (*Channel, *Channel) {
	t.Helper()
	ctx := context.Background()
	type acceptRes struct {
		ch  *Channel
		req OpenRequest
		err error
	}
	accCh := make(chan acceptRes, 1)
	guard.Go("livedoor.test.accept", func() {
		ch, req, err := h.Accept(ctx)
		if err == nil {
			err = ch.Accept()
		}
		accCh <- acceptRes{ch: ch, req: req, err: err}
	})

	vCh, err := v.Open(ctx, chat)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", chat, err)
	}
	res := <-accCh
	if res.err != nil {
		t.Fatalf("Accept(%q) failed: %v", chat, res.err)
	}
	if res.req.Chat != chat || res.req.V != 1 {
		t.Fatalf("OpenRequest = %+v, want chat=%q v=1", res.req, chat)
	}
	return vCh, res.ch
}

// TestEveryFrameOpRoundTripsOversizeFailsAndUnknownOpsAreSkipped verifies
// contract (mux): a frame round-trips through the codec for every op, an n
// above ChunkBytes is a protocol error, and an unknown op with a payload is
// skipped so the next frame still decodes.
func TestEveryFrameOpRoundTripsOversizeFailsAndUnknownOpsAreSkipped(t *testing.T) {
	ops := []uint8{
		OpOpen,
		OpOpened,
		OpRefused,
		OpData,
		OpCredit,
		OpClose,
		OpReset,
		OpPing,
		OpPong,
		OpHelloAck,
	}
	var buf bytes.Buffer
	for i, op := range ops {
		want := frame{
			op:      op,
			channel: uint32(i*2 + 1),
			payload: []byte{byte(i), 'o', 'k'},
		}
		if _, err := writeFrame(&buf, want, ChunkBytes); err != nil {
			t.Fatalf("writeFrame(op=%d) failed: %v", op, err)
		}
		got, err := readFrame(&buf, ChunkBytes, nil)
		if err != nil {
			t.Fatalf("readFrame(op=%d) failed: %v", op, err)
		}
		if got.op != want.op || got.channel != want.channel || !bytes.Equal(got.payload, want.payload) {
			t.Fatalf("round-trip op=%d got %+v, want %+v", op, got, want)
		}
	}

	// An unknown opcode with a payload is skipped and the following frame decodes.
	buf.Reset()
	unknownPayload := []byte("future-extension-payload")
	var hdr [frameHeaderBytes]byte
	hdr[0] = 99
	binary.BigEndian.PutUint32(hdr[1:5], 7)
	binary.BigEndian.PutUint32(hdr[5:9], uint32(len(unknownPayload)))
	buf.Write(hdr[:])
	buf.Write(unknownPayload)

	nextWant := frame{op: OpData, channel: 3, payload: []byte("after-unknown")}
	if _, err := writeFrame(&buf, nextWant, ChunkBytes); err != nil {
		t.Fatalf("writeFrame after unknown failed: %v", err)
	}
	var readBytes int
	got, err := readFrame(&buf, ChunkBytes, func(n int) { readBytes += n })
	if err != nil {
		t.Fatalf("readFrame after unknown failed: %v", err)
	}
	if got.op != nextWant.op || got.channel != nextWant.channel || !bytes.Equal(got.payload, nextWant.payload) {
		t.Fatalf("decoded frame after unknown = %+v, want %+v", got, nextWant)
	}
	if wantBytes := frameHeaderBytes + len(unknownPayload) + frameHeaderBytes + len(nextWant.payload); readBytes != wantBytes {
		t.Fatalf("onFrameRead counted %d bytes, want %d", readBytes, wantBytes)
	}

	// An incoming frame declaring n > ChunkBytes is a protocol error.
	buf.Reset()
	hdr[0] = OpData
	binary.BigEndian.PutUint32(hdr[1:5], 1)
	binary.BigEndian.PutUint32(hdr[5:9], uint32(ChunkBytes+1))
	buf.Write(hdr[:])
	if _, err := readFrame(&buf, ChunkBytes, nil); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("readFrame with n > ChunkBytes returned %v, want ErrFrameTooLarge", err)
	}
}

// TestTwoChannelsInterleaveOnOneLinkWithoutStarvingTheSmallerOne verifies
// contract (mux): two channels interleave on one link, both deliver their
// bytes in order, and neither is starved while the other has a large backlog
// (writing 1 MiB on A while B sends a small message delivers B before A finishes).
func TestTwoChannelsInterleaveOnOneLinkWithoutStarvingTheSmallerOne(t *testing.T) {
	lim := DefaultLimits()
	lim.Rate = 64 << 20
	lim.RateBurst = 64 << 20
	v, h := newMuxPair(t, MuxConfig{Limits: lim}, MuxConfig{Limits: lim})

	vA, hA := openAcceptedPair(t, v, h, "chat-a")
	vB, hB := openAcceptedPair(t, v, h, "chat-b")

	const totalA = 1 << 20
	wantA := make([]byte, totalA)
	for i := range wantA {
		wantA[i] = byte(i % 251)
	}
	wantB := []byte("urgent-small-message-on-b")

	aDone := make(chan error, 1)
	guard.Go("livedoor.test.writeA", func() {
		_, err := vA.Write(wantA)
		_ = vA.Close()
		aDone <- err
	})

	// Read the first chunk of A so A is actively mid-transfer with its remaining
	// window queued in A's outbound queue before B writes its small message.
	gotA := make([]byte, 0, totalA)
	firstChunk := make([]byte, ChunkBytes)
	if _, err := io.ReadFull(hA, firstChunk); err != nil {
		t.Fatalf("reading first chunk of A failed: %v", err)
	}
	gotA = append(gotA, firstChunk...)

	bWriteDone := make(chan error, 1)
	guard.Go("livedoor.test.writeB", func() {
		_, err := vB.Write(wantB)
		_ = vB.Close()
		bWriteDone <- err
	})

	gotB, err := io.ReadAll(hB)
	if err != nil {
		t.Fatalf("reading B failed: %v", err)
	}
	if !bytes.Equal(gotB, wantB) {
		t.Fatalf("B payload = %q, want %q", gotB, wantB)
	}

	// B must arrive while A still has the bulk of its 1 MiB transfer pending.
	select {
	case err := <-aDone:
		t.Fatalf("A finished its 1 MiB write before B was read (err=%v)", err)
	default:
	}

	restA, err := io.ReadAll(hA)
	if err != nil {
		t.Fatalf("reading remainder of A failed: %v", err)
	}
	gotA = append(gotA, restA...)
	if err := <-aDone; err != nil {
		t.Fatalf("vA.Write failed: %v", err)
	}
	if err := <-bWriteDone; err != nil {
		t.Fatalf("vB.Write failed: %v", err)
	}
	if !bytes.Equal(gotA, wantA) {
		t.Fatalf("A delivered %d bytes, want exact 1 MiB in order", len(gotA))
	}
}

// TestAChannelThatIsNotReadStopsTheSenderAtItsWindowAndOtherChannelsStillFlow
// verifies contract V17 (mux half): a reader that never reads one channel caps
// the sender at ChannelWindow bytes in flight, a second channel on the same
// link keeps flowing, and the stuck sender wakes when the reader finally reads.
func TestAChannelThatIsNotReadStopsTheSenderAtItsWindowAndOtherChannelsStillFlow(t *testing.T) {
	lim := DefaultLimits()
	lim.Rate = 64 << 20
	lim.RateBurst = 64 << 20
	v, h := newMuxPair(t, MuxConfig{Limits: lim}, MuxConfig{Limits: lim})

	vA, hA := openAcceptedPair(t, v, h, "unread-a")
	vB, hB := openAcceptedPair(t, v, h, "flowing-b")

	var aSent atomic.Int64
	aReachedWindow := make(chan struct{})
	aFinished := make(chan error, 1)
	chunk := bytes.Repeat([]byte{'a'}, ChunkBytes)
	const totalChunksA = (ChannelWindow / ChunkBytes) + 4

	guard.Go("livedoor.test.v17.writeA", func() {
		for i := 0; i < totalChunksA; i++ {
			n, err := vA.Write(chunk)
			if err != nil {
				aFinished <- err
				return
			}
			if aSent.Add(int64(n)) == int64(ChannelWindow) {
				close(aReachedWindow)
			}
		}
		aFinished <- nil
	})

	<-aReachedWindow

	// Push 512 KiB (two full ChannelWindows) across channel B while channel A
	// remains unread and pinned at ChannelWindow.
	const totalB = 2 * ChannelWindow
	wantB := bytes.Repeat([]byte{'b'}, totalB)
	bReadDone := make(chan []byte, 1)
	guard.Go("livedoor.test.v17.readB", func() {
		data, _ := io.ReadAll(hB)
		bReadDone <- data
	})
	if _, err := vB.Write(wantB); err != nil {
		t.Fatalf("vB.Write failed while A was capped: %v", err)
	}
	_ = vB.Close()
	if gotB := <-bReadDone; !bytes.Equal(gotB, wantB) {
		t.Fatalf("channel B read %d bytes, want %d", len(gotB), len(wantB))
	}

	if got := aSent.Load(); got != int64(ChannelWindow) {
		t.Fatalf("unread channel A sent %d bytes, want capped at ChannelWindow (%d)", got, ChannelWindow)
	}

	// Now drain channel A on the host and verify the stuck sender wakes and completes.
	wantTotalA := totalChunksA * ChunkBytes
	gotA := make([]byte, wantTotalA)
	if _, err := io.ReadFull(hA, gotA); err != nil {
		t.Fatalf("draining channel A failed: %v", err)
	}
	if err := <-aFinished; err != nil {
		t.Fatalf("vA.Write after drain returned error: %v", err)
	}
	if got := aSent.Load(); got != int64(wantTotalA) {
		t.Fatalf("channel A total sent = %d, want %d", got, wantTotalA)
	}
}

// TestACreditIsReturnedWhenTheQueueDrainsAndNeverFromATimer verifies contract
// V15 (mux half) / (mux): with a fake clock never advanced, the sender still
// gets credit back after the reader consumes, and no frame is written while
// idle for a virtual 10 minutes (OnFrameWritten never fires).
func TestACreditIsReturnedWhenTheQueueDrainsAndNeverFromATimer(t *testing.T) {
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	// FRAMES ARE COUNTED AT THE TRANSPORT, BEFORE THEY ARE DELIVERED. The mux's
	// OnFrameWritten runs after a write returns, and over a synchronous pipe the
	// receiver can have read the frame, returned the credit and woken this test
	// before that callback runs, so a count taken from it can still be rising
	// when the idle measurement starts. A count made before delivery cannot.
	var framesWritten atomic.Int64
	c1, c2 := net.Pipe()
	cfg := MuxConfig{Clock: fc}
	v := NewMux(&countBeforeRWC{ReadWriteCloser: c1, n: &framesWritten}, Viewer, cfg)
	h := NewMux(&countBeforeRWC{ReadWriteCloser: c2, n: &framesWritten}, Host, cfg)
	t.Cleanup(func() {
		_ = v.Close()
		_ = h.Close()
	})
	vCh, hCh := openAcceptedPair(t, v, h, "data-driven-credit")

	// Transfer 2 * ChannelWindow (512 KiB <= RateBurst 1 MiB) without ever
	// advancing the fake clock; credit must return purely from reader consumption.
	const transferBytes = 2 * ChannelWindow
	payload := bytes.Repeat([]byte{'d'}, transferBytes)
	readErrCh := make(chan error, 1)
	guard.Go("livedoor.test.v15.reader", func() {
		buf := make([]byte, transferBytes)
		_, err := io.ReadFull(hCh, buf)
		readErrCh <- err
	})

	if _, err := vCh.Write(payload); err != nil {
		t.Fatalf("Write across two windows without advancing clock failed: %v", err)
	}
	if err := <-readErrCh; err != nil {
		t.Fatalf("ReadFull across two windows failed: %v", err)
	}

	// Wait for the trailing channel and link credit frames from the last Read to
	// flush before measuring the 10-minute idle period.
	v.mu.Lock()
	for vCh.sendCredit < ChannelWindow || v.linkSendCredit < LinkWindow {
		v.cond.Wait()
	}
	v.mu.Unlock()

	framesWritten.Store(0)
	fc.Advance(10 * time.Minute)
	if got := framesWritten.Load(); got != 0 {
		t.Fatalf("%d frames were written during 10 virtual minutes of idle, want 0", got)
	}
}

// TestPeerOverrunningCreditFailsTheLinkCloseWakesBlockedCallersAndTooManyChannelsIsRefused
// verifies contract (mux): a peer that sends data beyond its credit fails the
// link, Close wakes every blocked Read/Write/Open/Accept exactly once, and
// opening past MaxChannelsPerLink is refused with too-many.
func TestPeerOverrunningCreditFailsTheLinkCloseWakesBlockedCallersAndTooManyChannelsIsRefused(t *testing.T) {
	// 1. Opening past MaxChannelsPerLink is refused with ReasonTooMany.
	v, h := newMuxPair(t, MuxConfig{}, MuxConfig{})
	for i := 0; i < MaxChannelsPerLink; i++ {
		openAcceptedPair(t, v, h, "slot")
	}
	_, err := v.Open(context.Background(), "one-too-many")
	var refErr *RefusedError
	if !errors.As(err, &refErr) || refErr.Reason != ReasonTooMany {
		t.Fatalf("33rd Open returned %v, want *RefusedError{Reason:%q}", err, ReasonTooMany)
	}

	// 2. A peer that sends DATA beyond its ChannelWindow credit fails the link.
	rawPeer, hostSide := net.Pipe()
	hostMux := NewMux(hostSide, Host, MuxConfig{})
	t.Cleanup(func() {
		_ = rawPeer.Close()
		_ = hostMux.Close()
	})
	accepted := make(chan *Channel, 1)
	guard.Go("livedoor.test.overrun.accept", func() {
		ch, _, err := hostMux.Accept(context.Background())
		if err == nil {
			_ = ch.Accept()
			accepted <- ch
		}
	})
	if _, err := writeFrame(rawPeer, frame{op: OpOpen, channel: 1, payload: []byte(`{"chat":"c","v":1}`)}, ChunkBytes); err != nil {
		t.Fatalf("raw OpOpen failed: %v", err)
	}
	if _, err := readFrame(rawPeer, ChunkBytes, nil); err != nil {
		t.Fatalf("reading OpOpened failed: %v", err)
	}
	hostCh := <-accepted

	chunk := make([]byte, ChunkBytes)
	chunksInWindow := ChannelWindow / ChunkBytes
	for i := 0; i < chunksInWindow; i++ {
		if _, err := writeFrame(rawPeer, frame{op: OpData, channel: 1, payload: chunk}, ChunkBytes); err != nil {
			t.Fatalf("writing within window failed at chunk %d: %v", i, err)
		}
	}
	// One extra byte beyond ChannelWindow without any consumed read must fail the link.
	_, _ = writeFrame(rawPeer, frame{op: OpData, channel: 1, payload: []byte{'!'}}, ChunkBytes)
	<-hostMux.Done()
	if _, err := hostCh.Write([]byte("x")); !errors.Is(err, ErrLinkClosed) {
		t.Fatalf("hostCh.Write after credit overrun returned %v, want ErrLinkClosed", err)
	}

	// 3. Close wakes every blocked Read, Write, Open, and Accept exactly once.
	v2, h2 := newMuxPair(t, MuxConfig{}, MuxConfig{})
	vCh2, hCh2 := openAcceptedPair(t, v2, h2, "wake-test")

	// Exhaust vCh2's channel send credit so the next Write blocks.
	if _, err := vCh2.Write(make([]byte, ChannelWindow)); err != nil {
		t.Fatalf("filling channel window failed: %v", err)
	}

	readWake := make(chan error, 1)
	writeWake := make(chan error, 1)
	openWake := make(chan error, 1)
	acceptWake := make(chan error, 1)

	// Drain hCh2 first so a subsequent Read blocks on an empty buffer.
	drained := make([]byte, ChannelWindow)
	if _, err := io.ReadFull(hCh2, drained); err != nil {
		t.Fatalf("draining hCh2 failed: %v", err)
	}
	// Refill window on a second channel where h2 never reads so Write blocks while hCh2 Read blocks.
	vCh3, _ := openAcceptedPair(t, v2, h2, "write-block")
	if _, err := vCh3.Write(make([]byte, ChannelWindow)); err != nil {
		t.Fatalf("filling vCh3 window failed: %v", err)
	}

	guard.Go("livedoor.test.wake.read", func() {
		var b [8]byte
		_, err := hCh2.Read(b[:])
		readWake <- err
	})
	guard.Go("livedoor.test.wake.write", func() {
		_, err := vCh3.Write([]byte("blocked-on-credit"))
		writeWake <- err
	})
	guard.Go("livedoor.test.wake.accept", func() {
		_, _, err := h2.Accept(context.Background())
		acceptWake <- err
	})

	// Use a raw peer so Open blocks waiting for OpOpened that never arrives.
	cOpen1, cOpen2 := net.Pipe()
	vOpen := NewMux(cOpen1, Viewer, MuxConfig{})
	t.Cleanup(func() {
		_ = cOpen2.Close()
		_ = vOpen.Close()
	})
	guard.Go("livedoor.test.wake.drainOpen", func() {
		_, _ = readFrame(cOpen2, ChunkBytes, nil)
	})
	guard.Go("livedoor.test.wake.open", func() {
		_, err := vOpen.Open(context.Background(), "never-answered")
		openWake <- err
	})

	_ = v2.Close()
	_ = v2.Close()
	_ = h2.Close()
	_ = vOpen.Close()

	if err := <-readWake; err == nil {
		t.Fatal("blocked Read woke with nil error on Close")
	}
	if err := <-writeWake; !errors.Is(err, ErrLinkClosed) {
		t.Fatalf("blocked Write woke with %v, want ErrLinkClosed", err)
	}
	if err := <-acceptWake; !errors.Is(err, ErrLinkClosed) {
		t.Fatalf("blocked Accept woke with %v, want ErrLinkClosed", err)
	}
	if err := <-openWake; !errors.Is(err, ErrLinkClosed) {
		t.Fatalf("blocked Open woke with %v, want ErrLinkClosed", err)
	}
}

// TestPingIsAnsweredWithMatchingPongAndAnIdleMuxWritesNothingOverAnHour
// verifies contract (mux): OpPing is answered with OpPong carrying the same seq,
// and a mux with no pings and no traffic writes zero frames over a virtual hour.
func TestPingIsAnsweredWithMatchingPongAndAnIdleMuxWritesNothingOverAnHour(t *testing.T) {
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	pongs := make(chan uint32, 1)
	var written atomic.Int64

	v, _ := newMuxPair(t,
		MuxConfig{
			Clock:          fc,
			OnPong:         func(seq uint32) { pongs <- seq },
			OnFrameWritten: func(int) { written.Add(1) },
		},
		MuxConfig{
			Clock:          fc,
			OnFrameWritten: func(int) { written.Add(1) },
		},
	)

	if err := v.SendPing(0xCAFEBABE); err != nil {
		t.Fatalf("SendPing failed: %v", err)
	}
	if got := <-pongs; got != 0xCAFEBABE {
		t.Fatalf("OnPong got seq %#x, want 0xCAFEBABE", got)
	}
	eventually(t, "both ping and pong frame write callbacks to complete", func() bool {
		return written.Load() == 2
	})

	written.Store(0)
	fc.Advance(time.Hour)
	if got := written.Load(); got != 0 {
		t.Fatalf("idle mux wrote %d frames over a virtual hour, want 0", got)
	}
}

// TestOneLinkIsPacedToItsRate verifies contract V18 (pacing half): with the
// fake clock, writing 3 MiB through a link with Rate = 1 MiB/s and burst 1 MiB
// delivers the first MiB at once and the rest only as the clock advances
// (asserting byte totals at virtual 0 s, 1 s, and 2 s).
func TestOneLinkIsPacedToItsRate(t *testing.T) {
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	lim := DefaultLimits()
	lim.Rate = 1 << 20
	lim.RateBurst = 1 << 20

	v, h := newMuxPair(t, MuxConfig{Clock: fc, Limits: lim}, MuxConfig{Clock: fc, Limits: lim})
	vCh, hCh := openAcceptedPair(t, v, h, "paced-chat")

	const (
		oneMiB   = 1 << 20
		threeMiB = 3 << 20
	)
	var delivered atomic.Int64
	at1MiB := make(chan struct{})
	at2MiB := make(chan struct{})
	at3MiB := make(chan struct{})

	guard.Go("livedoor.test.v18.reader", func() {
		buf := make([]byte, ChunkBytes)
		for {
			n, err := hCh.Read(buf)
			if n > 0 {
				tot := delivered.Add(int64(n))
				if tot == oneMiB {
					close(at1MiB)
				} else if tot == 2*oneMiB {
					close(at2MiB)
				} else if tot == threeMiB {
					close(at3MiB)
				}
			}
			if err != nil {
				return
			}
		}
	})

	writeDone := make(chan error, 1)
	guard.Go("livedoor.test.v18.writer", func() {
		_, err := vCh.Write(make([]byte, threeMiB))
		writeDone <- err
	})

	// Virtual 0 s: the 1 MiB burst arrives immediately and the writer parks on
	// the fake clock waiting for tokens for the second megabyte.
	<-at1MiB
	fc.WaitForTimers(1)
	if got := delivered.Load(); got != oneMiB {
		t.Fatalf("delivered at virtual 0 s = %d, want %d (1 MiB)", got, oneMiB)
	}

	// Virtual 1 s: advancing by 1 s refills 1 MiB and delivers the second megabyte.
	fc.Advance(time.Second)
	<-at2MiB
	fc.WaitForTimers(1)
	if got := delivered.Load(); got != 2*oneMiB {
		t.Fatalf("delivered at virtual 1 s = %d, want %d (2 MiB)", got, 2*oneMiB)
	}

	// Virtual 2 s: advancing by another 1 s refills 1 MiB and delivers the third megabyte.
	fc.Advance(time.Second)
	<-at3MiB
	if err := <-writeDone; err != nil {
		t.Fatalf("3 MiB write returned error: %v", err)
	}
	if got := delivered.Load(); got != threeMiB {
		t.Fatalf("delivered at virtual 2 s = %d, want %d (3 MiB)", got, threeMiB)
	}
}

// TestRefuseReachesOpenAsRefusedErrorAndResetCarriesItsReasonToRead verifies
// contract (mux): Refuse("not-running") reaches the viewer's Open as
// *RefusedError{Reason:"not-running"}, and Reset("lagging") on the host side
// makes the viewer's channel Read return an error whose Reason() is lagging.
func TestRefuseReachesOpenAsRefusedErrorAndResetCarriesItsReasonToRead(t *testing.T) {
	v, h := newMuxPair(t, MuxConfig{}, MuxConfig{})

	// 1. Refuse("not-running") reaches Open as *RefusedError{Reason:"not-running"}.
	guard.Go("livedoor.test.refuse", func() {
		ch, _, err := h.Accept(context.Background())
		if err == nil {
			_ = ch.Refuse(ReasonNotRunning)
		}
	})
	_, err := v.Open(context.Background(), "stopped-chat")
	var refErr *RefusedError
	if !errors.As(err, &refErr) || refErr.Reason != ReasonNotRunning {
		t.Fatalf("Open after Refuse(%q) returned %v, want *RefusedError{Reason:%q}", ReasonNotRunning, err, ReasonNotRunning)
	}

	// 2. Reset("lagging") on the host makes the viewer's channel Read return an
	// error whose Reason() is "lagging".
	vCh, hCh := openAcceptedPair(t, v, h, "lagging-chat")
	if err := hCh.Reset(ReasonLagging); err != nil {
		t.Fatalf("hCh.Reset(%q) failed: %v", ReasonLagging, err)
	}
	var buf [32]byte
	_, readErr := vCh.Read(buf[:])
	if readErr == nil {
		t.Fatal("vCh.Read after host Reset succeeded, want error")
	}
	var reasoner interface{ Reason() string }
	if !errors.As(readErr, &reasoner) || reasoner.Reason() != ReasonLagging {
		t.Fatalf("vCh.Read error = %T (%v), want Reason() == %q", readErr, readErr, ReasonLagging)
	}
	if got := vCh.Reason(); got != ReasonLagging {
		t.Fatalf("vCh.Reason() = %q, want %q", got, ReasonLagging)
	}

	// 3. OpHelloAck round-trips from host to viewer on channel 0.
	wantAck := HelloAck{
		V:      1,
		Watch:  true,
		Drive:  false,
		Road:   "relay",
		Limits: DefaultLimits(),
	}
	if err := h.SendHelloAck(wantAck); err != nil {
		t.Fatalf("SendHelloAck failed: %v", err)
	}
	gotAck, err := v.WaitHelloAck(context.Background())
	if err != nil {
		t.Fatalf("WaitHelloAck failed: %v", err)
	}
	if gotAck.Watch != wantAck.Watch || gotAck.Drive != wantAck.Drive || gotAck.Road != wantAck.Road || gotAck.Limits.ChannelWindow != wantAck.Limits.ChannelWindow {
		t.Fatalf("WaitHelloAck got %+v, want %+v", gotAck, wantAck)
	}
}

// TestUnixTransportEnforcesSunPathLimitAndDialsShortSocket verifies contract
// (mux): UnixTransport rejects socket paths over 103 bytes and listens/dials
// short socket paths under /tmp/ld-*.
func TestUnixTransportEnforcesSunPathLimitAndDialsShortSocket(t *testing.T) {
	tr := UnixTransport()
	longAddr := "/tmp/" + strings.Repeat("a", 100)
	if _, err := tr.Listen(longAddr); err == nil {
		t.Fatalf("Listen(%d-byte path) succeeded, want sun_path limit error", len(longAddr))
	}
	if _, err := tr.Dial(context.Background(), longAddr); err == nil {
		t.Fatalf("Dial(%d-byte path) succeeded, want sun_path limit error", len(longAddr))
	}

	dir, err := os.MkdirTemp("/tmp", "ld-")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	addr := filepath.Join(dir, "s.sock")
	ln, err := tr.Listen(addr)
	if err != nil {
		t.Fatalf("Listen(%q) failed: %v", addr, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accepted := make(chan net.Conn, 1)
	guard.Go("livedoor.test.unix.accept", func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	})

	client, err := tr.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("Dial(%q) failed: %v", addr, err)
	}
	t.Cleanup(func() { _ = client.Close() })

	server := <-accepted
	t.Cleanup(func() { _ = server.Close() })
}
