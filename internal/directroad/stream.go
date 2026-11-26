package directroad

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// maxReadMessageBytes is the 64 KiB receive buffer capacity required by the
	// road contract; any single data-channel message larger than this is a
	// protocol violation that terminates the stream.
	maxReadMessageBytes = 64 << 10

	// maxWriteMessageBytes caps each outbound data-channel message at 16 KiB so
	// large writes are sliced into bounded SCTP messages well under the
	// receiver's 64 KiB ceiling.
	maxWriteMessageBytes = 16 << 10
)

// errMessageTooLarge reports an inbound data-channel message that exceeded
// maxReadMessageBytes; it deliberately does not wrap io.ErrShortBuffer because
// Read on the byte stream must never report a short buffer to callers.
var errMessageTooLarge = fmt.Errorf("directroad: inbound message exceeds %d-byte limit", maxReadMessageBytes)

// messageConn is the minimal message-oriented read/write/close interface
// satisfied by pion's detached data channel and by test fakes.
type messageConn interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}

// byteStream adapts a message-oriented data channel into a continuous
// io.ReadWriteCloser byte pipe.
type byteStream struct {
	raw     messageConn
	closeFn func()

	readMu   sync.Mutex
	readBuf  [maxReadMessageBytes]byte
	readOff  int
	readEnd  int
	fatalErr error

	writeMu sync.Mutex

	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

// newByteStream wraps raw in a byte-stream adapter that invokes closeFn once
// after closing raw.
func newByteStream(raw messageConn, closeFn func()) *byteStream {
	return &byteStream{
		raw:     raw,
		closeFn: closeFn,
	}
}

// Read serves bytes from the unread tail of the current message or pulls the
// next non-empty message into the 64 KiB staging buffer, never exposing
// message boundaries or io.ErrShortBuffer to the caller.
func (s *byteStream) Read(p []byte) (int, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()

	if s.closed.Load() {
		if s.fatalErr != nil {
			return 0, s.fatalErr
		}
		return 0, io.EOF
	}

	if s.readOff < s.readEnd {
		n := copy(p, s.readBuf[s.readOff:s.readEnd])
		s.readOff += n
		return n, nil
	}

	if len(p) == 0 {
		return 0, nil
	}

	for {
		if s.closed.Load() {
			if s.fatalErr != nil {
				return 0, s.fatalErr
			}
			return 0, io.EOF
		}

		n, err := s.raw.Read(s.readBuf[:])
		if errors.Is(err, io.ErrShortBuffer) {
			// A message over 64 KiB violates the road contract; record a plain
			// error and close the stream so neither side continues on a torn
			// message boundary.
			s.fatalErr = errMessageTooLarge
			_ = s.Close()
			return 0, errMessageTooLarge
		}
		if err != nil {
			if s.closed.Load() && s.fatalErr == nil {
				return 0, io.EOF
			}
			return 0, err
		}
		if n == 0 {
			// Zero-length data-channel messages carry no stream bytes and must
			// never surface as an ambiguous (0, nil) read.
			continue
		}

		copied := copy(p, s.readBuf[:n])
		s.readOff = copied
		s.readEnd = n
		return copied, nil
	}
}

// Write slices p into messages of at most maxWriteMessageBytes (16 KiB) and
// serialises concurrent writers with a mutex.
func (s *byteStream) Write(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	total := 0
	for len(p) > 0 {
		if s.closed.Load() {
			return total, io.ErrClosedPipe
		}
		chunk := p
		if len(chunk) > maxWriteMessageBytes {
			chunk = p[:maxWriteMessageBytes]
		}
		n, err := s.raw.Write(chunk)
		total += n
		if err != nil {
			return total, err
		}
		p = p[len(chunk):]
	}
	return total, nil
}

// Close closes the underlying message channel and then the peer connection
// exactly once, waking any blocked Read or Write immediately.
func (s *byteStream) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if d, ok := s.raw.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = d.SetReadDeadline(time.Now())
		}
		if d, ok := s.raw.(interface{ SetWriteDeadline(time.Time) error }); ok {
			_ = d.SetWriteDeadline(time.Now())
		}
		s.closeErr = s.raw.Close()
		if s.closeFn != nil {
			s.closeFn()
		}
	})
	return s.closeErr
}
