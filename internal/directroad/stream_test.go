package directroad

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// fakeMessageConn emulates pion's detached data channel: each Read pops one
// whole message and returns io.ErrShortBuffer (discarding the unread remainder
// of that message) if the caller's buffer is smaller than the message.
type fakeMessageConn struct {
	mu         sync.Mutex
	cond       *sync.Cond
	inbox      [][]byte
	written    [][]byte
	closed     bool
	closeCalls int
}

func newFakeMessageConn() *fakeMessageConn {
	f := &fakeMessageConn{}
	f.cond = sync.NewCond(&f.mu)
	return f
}

func (f *fakeMessageConn) enqueue(msg []byte) {
	f.mu.Lock()
	cp := append([]byte(nil), msg...)
	f.inbox = append(f.inbox, cp)
	f.cond.Signal()
	f.mu.Unlock()
}

func (f *fakeMessageConn) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for len(f.inbox) == 0 && !f.closed {
		f.cond.Wait()
	}
	if len(f.inbox) == 0 {
		return 0, io.EOF
	}

	msg := f.inbox[0]
	f.inbox[0] = nil
	f.inbox = f.inbox[1:]

	if len(p) < len(msg) {
		n := copy(p, msg)
		return n, io.ErrShortBuffer
	}
	n := copy(p, msg)
	return n, nil
}

func (f *fakeMessageConn) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, io.ErrClosedPipe
	}
	cp := append([]byte(nil), p...)
	f.written = append(f.written, cp)
	return len(cp), nil
}

func (f *fakeMessageConn) Close() error {
	f.mu.Lock()
	f.closed = true
	f.closeCalls++
	f.cond.Broadcast()
	f.mu.Unlock()
	return nil
}

// Contract: stream adapter turns a message-oriented detached data channel into
// a byte pipe that slices writes at 16 KiB, stages reads up to 64 KiB, skips
// empty messages, rejects >64 KiB messages as a protocol violation without
// returning io.ErrShortBuffer, and closes idempotently on Close or connection
// state Failed.
func TestAStreamAdapterServesABytePipeFromMessages(t *testing.T) {
	// 1. A 100 KiB Write is sliced into <=16 KiB messages whose concatenation
	// equals the input, and an empty Write sends nothing.
	t.Run("write_slicing_and_one_byte_reads", func(t *testing.T) {
		fc := newFakeMessageConn()
		var closeFnCalls atomic.Int32
		s := newByteStream(fc, func() { closeFnCalls.Add(1) })

		if n, err := s.Write(nil); err != nil || n != 0 {
			t.Fatalf("empty Write = (%d, %v), want (0, nil)", n, err)
		}
		if len(fc.written) != 0 {
			t.Fatalf("empty Write sent %d messages, want 0", len(fc.written))
		}

		const writeSize = 100 << 10
		input := make([]byte, writeSize)
		for i := range input {
			input[i] = byte((i*31 + 7) & 0xff)
		}

		n, err := s.Write(input)
		if err != nil || n != len(input) {
			t.Fatalf("Write(100 KiB) = (%d, %v), want (%d, nil)", n, err, len(input))
		}
		if len(fc.written) < 2 {
			t.Fatalf("expected multiple messages for 100 KiB write, got %d", len(fc.written))
		}
		var joined []byte
		for i, msg := range fc.written {
			if len(msg) == 0 || len(msg) > maxWriteMessageBytes {
				t.Fatalf("message %d has length %d, want 1..%d", i, len(msg), maxWriteMessageBytes)
			}
			joined = append(joined, msg...)
		}
		if !bytes.Equal(joined, input) {
			t.Fatal("concatenated outbound messages do not match 100 KiB input")
		}

		// Queue 70,000 bytes across messages (as produced by a sender or up to
		// 64 KiB chunks) and read them back one byte at a time in order.
		const streamTotal = 70_000
		want := make([]byte, streamTotal)
		for i := range want {
			want[i] = byte((i*17 + 3) & 0xff)
		}
		fc.enqueue(want[:maxReadMessageBytes])
		fc.enqueue(want[maxReadMessageBytes:])

		got := make([]byte, streamTotal)
		var one [1]byte
		for i := range streamTotal {
			rn, rerr := s.Read(one[:])
			if errors.Is(rerr, io.ErrShortBuffer) {
				t.Fatalf("byte %d: Read returned io.ErrShortBuffer", i)
			}
			if rerr != nil || rn != 1 {
				t.Fatalf("byte %d: Read = (%d, %v), want (1, nil)", i, rn, rerr)
			}
			got[i] = one[0]
		}
		if !bytes.Equal(got, want) {
			t.Fatal("70,000 one-byte reads did not match queued bytes in order")
		}
	})

	// 2. Noise reader pattern: 2-byte big-endian length prefix followed by the
	// body in a single data-channel message, with a zero-length message before
	// it that must be skipped transparently.
	t.Run("prefix_then_body_and_skip_zero_length", func(t *testing.T) {
		fc := newFakeMessageConn()
		s := newByteStream(fc, nil)

		body := bytes.Repeat([]byte("noise-record-payload-"), 120)
		frame := make([]byte, 2+len(body))
		binary.BigEndian.PutUint16(frame[:2], uint16(len(body)))
		copy(frame[2:], body)

		fc.enqueue([]byte{})
		fc.enqueue(frame)

		var hdr [2]byte
		if _, err := io.ReadFull(s, hdr[:]); err != nil {
			t.Fatalf("io.ReadFull header: %v", err)
		}
		gotLen := int(binary.BigEndian.Uint16(hdr[:]))
		if gotLen != len(body) {
			t.Fatalf("header length = %d, want %d", gotLen, len(body))
		}

		gotBody := make([]byte, gotLen)
		if _, err := io.ReadFull(s, gotBody); err != nil {
			t.Fatalf("io.ReadFull body: %v", err)
		}
		if !bytes.Equal(gotBody, body) {
			t.Fatal("body read after 2-byte prefix does not match")
		}
	})

	// 3. A single message over 64 KiB (e.g. 70,000 bytes in one message) causes
	// the underlying channel to report io.ErrShortBuffer, which the adapter
	// turns into a non-ErrShortBuffer protocol violation error and closes the
	// stream idempotently.
	t.Run("oversized_message_violation_and_idempotent_close", func(t *testing.T) {
		fc := newFakeMessageConn()
		var closeFnCalls atomic.Int32
		s := newByteStream(fc, func() { closeFnCalls.Add(1) })

		fc.enqueue(make([]byte, 70_000))

		buf := make([]byte, 32)
		_, err := s.Read(buf)
		if err == nil {
			t.Fatal("expected error reading >64 KiB message, got nil")
		}
		if errors.Is(err, io.ErrShortBuffer) {
			t.Fatalf("Read leaked io.ErrShortBuffer on oversized message: %v", err)
		}

		// Subsequent Close calls must be idempotent.
		if err := s.Close(); err != nil {
			t.Fatalf("first explicit Close: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("second explicit Close: %v", err)
		}
		if got := closeFnCalls.Load(); got != 1 {
			t.Fatalf("closeFn called %d times, want 1", got)
		}
		fc.mu.Lock()
		rawCloses := fc.closeCalls
		fc.mu.Unlock()
		if rawCloses != 1 {
			t.Fatalf("raw.Close called %d times, want 1", rawCloses)
		}
	})

	// 4. A PeerConnectionStateFailed callback closes the stream and wakes a
	// blocked Read immediately.
	t.Run("failed_state_callback_wakes_blocked_read", func(t *testing.T) {
		fc := newFakeMessageConn()
		var closeFnCalls atomic.Int32
		st := newByteStream(fc, func() { closeFnCalls.Add(1) })

		sess := &pcSession{
			openCh: make(chan io.ReadWriteCloser, 1),
			errCh:  make(chan error, 1),
			stream: st,
		}

		readErrCh := make(chan error, 1)
		go func() {
			var b [16]byte
			_, err := st.Read(b[:])
			readErrCh <- err
		}()

		// Disconnected must NOT close the stream because ICE can recover.
		sess.onConnectionState(webrtc.PeerConnectionStateDisconnected)
		select {
		case err := <-readErrCh:
			t.Fatalf("Disconnected state should not close stream, got Read err: %v", err)
		default:
		}

		// Failed MUST close the stream and wake the blocked Read.
		sess.onConnectionState(webrtc.PeerConnectionStateFailed)
		select {
		case err := <-readErrCh:
			if err == nil {
				t.Fatal("expected blocked Read to return an error on Failed state, got nil")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for Failed state callback to wake blocked Read")
		}
		if got := closeFnCalls.Load(); got != 1 {
			t.Fatalf("closeFn called %d times after Failed state, want 1", got)
		}
	})
}
