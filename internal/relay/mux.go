package relay

// THE ENGINE MACHINE DIALS OUT ONCE AND STAYS. Everything that ever reaches it
// through the relay therefore has to travel down that one connection, which is
// what this file is: a very small multiplexer, so that a desk and a phone
// attached to the same machine at the same time are two streams on one carrier
// rather than two carriers.
//
// IT IS DELIBERATELY THE SMALLEST MUX THAT WORKS. No windows, no flow control,
// no reordering — the carrier is a single ordered TCP connection and the frames
// on it are already in order, so a stream is a byte queue with an id in front of
// it. The one thing it does owe is BACKPRESSURE, and it gets that the honest
// way: a stream's reader is handed frames through a bounded channel, and the
// carrier's reader blocks when that channel is full, which stops the carrier. A
// surface that has stopped reading therefore slows the machine down rather than
// growing the relay's memory without a bound, which is the failure a relay
// operator cannot be asked to absorb.
//
// EVERY PAYLOAD ON THIS CARRIER IS OPAQUE. opData's bytes are the tunnel's
// ciphertext; nothing in this file decodes them, and nothing in this file could.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// The frame operations. They are a single byte because the header is read
// before anything is known about the frame and a fixed header is a fixed read.
const (
	opOpen  byte = 1 // the relay tells the machine a surface has arrived
	opData  byte = 2 // ciphertext, either direction
	opClose byte = 3 // this stream is over, either direction
	opPing  byte = 4 // the relay testing a silent carrier
	opPong  byte = 5 // the machine answering
)

// frameHeader is one byte of operation, eight of stream id, four of length.
const frameHeader = 1 + 8 + 4

// errCarrierGone is what every stream on a carrier reports once the carrier
// itself has ended. It is one error rather than one per stream so that a
// surface's read and a surface's write agree about what happened.
var errCarrierGone = errors.New("the connection to the relay ended")

// writeFrame puts one frame on a carrier in one write, so a carrier that is a
// WebSocket carries each frame as one message (see transport.go). The frame is
// assembled in scratch, which is handed back for the next frame so a carrier
// does not allocate one per write. Callers hold the carrier's write mutex,
// which also guards scratch.
func writeFrame(w io.Writer, scratch []byte, op byte, stream uint64, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayload {
		return scratch, fmt.Errorf("relay: frame of %d bytes is over the %d-byte limit", len(payload), MaxPayload)
	}
	frame := append(scratch[:0], op)
	frame = binary.BigEndian.AppendUint64(frame, stream)
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
	frame = append(frame, payload...)
	_, err := w.Write(frame)
	return frame, err
}

// readFrame takes one frame off a carrier. The payload is freshly allocated
// because it is handed to another goroutine and outlives this call.
func readFrame(r io.Reader) (op byte, stream uint64, payload []byte, err error) {
	var header [frameHeader]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[9:13])
	if length > MaxPayload {
		// A LENGTH OVER THE CEILING ENDS THE CARRIER rather than being skipped.
		// The framing is the only thing keeping the two ends in step, and a
		// peer that has sent an impossible length is a peer whose next byte is
		// not where this end thinks it is.
		return 0, 0, nil, fmt.Errorf("relay: frame claims %d bytes, over the %d-byte limit", length, MaxPayload)
	}
	if length > 0 {
		payload = make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, 0, nil, err
		}
	}
	return header[0], binary.BigEndian.Uint64(header[1:9]), payload, nil
}

// streamDepth is how many frames a stream may hold before the carrier stops
// reading. Small on purpose — see the backpressure paragraph in the header.
const streamDepth = 8

// muxStream is one surface's byte stream, presented as the [io.ReadWriteCloser]
// everything above this package deals in. It carries real read and write
// deadlines so a handshake above this package can evict a silent caller instead
// of letting it hold one of a machine's [MaxStreams] slots forever.
type muxStream struct {
	id      uint64
	carrier *carrier

	incoming chan []byte
	rest     []byte

	readDeadline  streamDeadline
	writeDeadline streamDeadline

	// closed is THIS END letting go, and incoming being closed is the FAR END
	// letting go. THE TWO ARE DELIBERATELY NOT THE SAME SIGNAL, and conflating
	// them was a real bug: a reader selecting on both picks between two ready
	// cases at random, so a stream that was written to and then closed lost its
	// last frames about half the time — which is exactly the shape of a
	// refusal, written and then hung up on. A far end that has finished
	// speaking closes only the queue, so a reader drains what arrived and THEN
	// sees the end.
	closeOnce sync.Once
	closed    chan struct{}
	endOnce   sync.Once
}

func newMuxStream(id uint64, c *carrier) *muxStream {
	return &muxStream{
		id:            id,
		carrier:       c,
		incoming:      make(chan []byte, streamDepth),
		readDeadline:  newStreamDeadline(),
		writeDeadline: newStreamDeadline(),
		closed:        make(chan struct{}),
	}
}

func (s *muxStream) SetDeadline(t time.Time) error {
	s.readDeadline.set(t)
	s.writeDeadline.set(t)
	return nil
}

func (s *muxStream) SetReadDeadline(t time.Time) error {
	s.readDeadline.set(t)
	return nil
}

func (s *muxStream) SetWriteDeadline(t time.Time) error {
	s.writeDeadline.set(t)
	return nil
}

func (s *muxStream) stopDeadlines() {
	s.readDeadline.set(time.Time{})
	s.writeDeadline.set(time.Time{})
}

func (s *muxStream) Read(b []byte) (int, error) {
	if isClosedChan(s.readDeadline.wait()) {
		return 0, os.ErrDeadlineExceeded
	}
	for len(s.rest) == 0 {
		select {
		case chunk, ok := <-s.incoming:
			if !ok {
				// A CLOSED QUEUE IS EOF AND NOT AN ERROR. The far end shut this
				// stream cleanly; everything above reads that the way it reads
				// an ssh child's stdout ending.
				return 0, io.EOF
			}
			s.rest = chunk
		case <-s.closed:
			return 0, io.EOF
		case <-s.readDeadline.wait():
			return 0, os.ErrDeadlineExceeded
		}
	}
	n := copy(b, s.rest)
	s.rest = s.rest[n:]
	return n, nil
}

func (s *muxStream) Write(b []byte) (int, error) {
	select {
	case <-s.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	if isClosedChan(s.writeDeadline.wait()) {
		return 0, os.ErrDeadlineExceeded
	}
	// A write is cut into carrier-sized frames, because a caller above knows
	// nothing about this framing and must not have to.
	written := 0
	for len(b) > 0 {
		select {
		case <-s.closed:
			return written, io.ErrClosedPipe
		case <-s.writeDeadline.wait():
			return written, os.ErrDeadlineExceeded
		default:
		}
		chunk := b
		if len(chunk) > MaxPayload {
			chunk = chunk[:MaxPayload]
		}
		if err := s.carrier.send(opData, s.id, chunk); err != nil {
			return written, err
		}
		written += len(chunk)
		b = b[len(chunk):]
	}
	return written, nil
}

func (s *muxStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.stopDeadlines()
		_ = s.carrier.send(opClose, s.id, nil)
		s.carrier.forget(s.id)
	})
	return nil
}

// streamDeadline signals when a stream's read or write deadline has passed, in
// the shape of net.Pipe's pipeDeadline so blocked callers wake at once and
// cleared deadlines hold no timer.
type streamDeadline struct {
	mu     sync.Mutex
	timer  *time.Timer
	cancel chan struct{}
}

func newStreamDeadline() streamDeadline {
	return streamDeadline{cancel: make(chan struct{})}
}

func (d *streamDeadline) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil && !d.timer.Stop() {
		<-d.cancel
	}
	d.timer = nil

	closed := isClosedChan(d.cancel)
	if t.IsZero() {
		if closed {
			d.cancel = make(chan struct{})
		}
		return
	}
	if dur := time.Until(t); dur > 0 {
		if closed {
			d.cancel = make(chan struct{})
		}
		cancel := d.cancel
		d.timer = time.AfterFunc(dur, func() {
			close(cancel)
		})
		return
	}
	if !closed {
		close(d.cancel)
	}
}

func (d *streamDeadline) wait() chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cancel
}

func isClosedChan(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// deliver hands a frame to this stream's reader, or drops it on the floor once
// the stream is closed — which is not a loss, because a closed stream has no
// reader and the peer will be told.
func (s *muxStream) deliver(payload []byte) {
	select {
	case s.incoming <- payload:
	case <-s.closed:
	}
}

// finish is the far end closing this stream. It closes the QUEUE and not the
// stream, so a reader drains everything that already arrived and only then sees
// the end — see the note on [muxStream.closed].
func (s *muxStream) finish() {
	s.endOnce.Do(func() {
		close(s.incoming)
		s.carrier.forget(s.id)
	})
}

// carrier is one machine's outbound connection and the streams riding it.
type carrier struct {
	conn io.ReadWriteCloser

	writeMu sync.Mutex
	scratch []byte // the frame being written, reused under writeMu

	mu      sync.Mutex
	streams map[uint64]*muxStream
	probes  map[chan struct{}]struct{}
	nextID  uint64
	gone    bool

	// accepted is where a machine's own registration hands it the surfaces
	// that arrived. It is nil on the relay's side of the carrier, where opOpen
	// is minted rather than received.
	accepted chan *muxStream

	// onFrame is called after every frame that arrives, and is how a carrier's
	// owner pushes its read deadline out. IT IS DRIVEN BY ARRIVALS AND NOT BY A
	// TIMER, which is the difference between a deadline that detects a silent
	// connection and one that can never fire: a ticker that reset the deadline
	// on its own schedule would keep a dead NAT mapping alive for ever.
	onFrame func()

	closeOnce sync.Once
	done      chan struct{}
	err       error
}

func newCarrier(conn io.ReadWriteCloser, accepts bool) *carrier {
	c := &carrier{
		conn:    conn,
		streams: map[uint64]*muxStream{},
		done:    make(chan struct{}),
	}
	if accepts {
		c.accepted = make(chan *muxStream, MaxStreams)
	}
	return c
}

func (c *carrier) send(op byte, stream uint64, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	gone := c.gone
	c.mu.Unlock()
	if gone {
		return errCarrierGone
	}
	scratch, err := writeFrame(c.conn, c.scratch, op, stream, payload)
	c.scratch = scratch
	if err != nil {
		return err
	}
	return nil
}

// open mints a stream on this carrier and tells the far end about it. Only the
// relay side calls it; a machine never dials a surface.
func (c *carrier) open() (*muxStream, error) {
	c.mu.Lock()
	if c.gone {
		c.mu.Unlock()
		return nil, errCarrierGone
	}
	if len(c.streams) >= MaxStreams {
		c.mu.Unlock()
		return nil, fmt.Errorf("that machine already has %d connections open, which is as many as the relay carries at once", MaxStreams)
	}
	c.nextID++
	stream := newMuxStream(c.nextID, c)
	c.streams[stream.id] = stream
	c.mu.Unlock()

	if err := c.send(opOpen, stream.id, nil); err != nil {
		c.forget(stream.id)
		return nil, err
	}
	return stream, nil
}

func (c *carrier) forget(id uint64) {
	c.mu.Lock()
	delete(c.streams, id)
	c.mu.Unlock()
}

func (c *carrier) lookup(id uint64) *muxStream {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams[id]
}

// pump is the carrier's single reader. It runs until the connection ends, and
// its ending is what tears every stream on it down.
func (c *carrier) pump() {
	defer c.shutdown(nil)
	for {
		op, id, payload, err := readFrame(c.conn)
		if err != nil {
			c.shutdown(err)
			return
		}
		if c.onFrame != nil {
			c.onFrame()
		}
		switch op {
		case opOpen:
			// A surface has arrived. Only a machine's own carrier accepts.
			if c.accepted == nil {
				c.shutdown(errors.New("relay: a machine opened a stream, which only the relay may do"))
				return
			}
			stream := newMuxStream(id, c)
			c.mu.Lock()
			c.streams[id] = stream
			c.mu.Unlock()
			select {
			case c.accepted <- stream:
			case <-c.done:
				return
			}
		case opData:
			if stream := c.lookup(id); stream != nil {
				stream.deliver(payload)
			}
		case opClose:
			if stream := c.lookup(id); stream != nil {
				stream.finish()
			}
		case opPing:
			_ = c.send(opPong, id, nil)
		case opPong:
			c.mu.Lock()
			for ch := range c.probes {
				close(ch)
				delete(c.probes, ch)
			}
			c.mu.Unlock()
		}
	}
}

// probe tests whether an older carrier under the same key is still answering,
// waiting at most within for its pong. A live machine answers in one round
// trip; one whose network vanished says nothing and lets its successor in.
func (c *carrier) probe(within time.Duration) bool {
	ch := make(chan struct{})
	c.mu.Lock()
	if c.gone {
		c.mu.Unlock()
		return false
	}
	if c.probes == nil {
		c.probes = map[chan struct{}]struct{}{}
	}
	c.probes[ch] = struct{}{}
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.probes, ch)
		c.mu.Unlock()
	}()

	if err := c.send(opPing, 0, nil); err != nil {
		return false
	}
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-c.done:
		return false
	case <-timer.C:
		return false
	}
}

// shutdown ends the carrier and everything on it, once.
func (c *carrier) shutdown(cause error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.gone = true
		streams := make([]*muxStream, 0, len(c.streams))
		for _, stream := range c.streams {
			streams = append(streams, stream)
		}
		c.streams = map[uint64]*muxStream{}
		c.mu.Unlock()

		c.err = cause
		// THE DONE CHANNEL IS THE ONLY THING CLOSED HERE, and c.accepted is
		// deliberately left open. Shutdown can be called from the carrier's own
		// reader and from whoever holds the registration, so closing the accept
		// queue would be a send on a closed channel every time those two raced;
		// every reader of it selects on done as well, which is the same signal
		// without the race.
		close(c.done)
		for _, stream := range streams {
			stream.closeOnce.Do(func() {
				close(stream.closed)
				stream.stopDeadlines()
			})
		}
		_ = c.conn.Close()
	})
}

// keepAlive pokes a silent carrier so that a NAT which quietly forgot the
// mapping is discovered rather than waited on for ever.
func (c *carrier) keepAlive() {
	ticker := time.NewTicker(PingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.send(opPing, 0, nil); err != nil {
				return
			}
		}
	}
}
