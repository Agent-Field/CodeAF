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

// writeFrame puts one frame on a carrier. Callers hold the carrier's write
// mutex; the mutex is not taken here because a frame is sometimes written as
// part of a longer critical section.
func writeFrame(w io.Writer, op byte, stream uint64, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("relay: frame of %d bytes is over the %d-byte limit", len(payload), MaxPayload)
	}
	var header [frameHeader]byte
	header[0] = op
	binary.BigEndian.PutUint64(header[1:9], stream)
	binary.BigEndian.PutUint32(header[9:13], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
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
// everything above this package deals in.
type muxStream struct {
	id      uint64
	carrier *carrier

	incoming chan []byte
	rest     []byte

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
		id:       id,
		carrier:  c,
		incoming: make(chan []byte, streamDepth),
		closed:   make(chan struct{}),
	}
}

func (s *muxStream) Read(b []byte) (int, error) {
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
	// A write is cut into carrier-sized frames, because a caller above knows
	// nothing about this framing and must not have to.
	written := 0
	for len(b) > 0 {
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
		_ = s.carrier.send(opClose, s.id, nil)
		s.carrier.forget(s.id)
	})
	return nil
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

	mu      sync.Mutex
	streams map[uint64]*muxStream
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
	if err := writeFrame(c.conn, op, stream, payload); err != nil {
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
			// Nothing to do: the frame arriving is the whole of the answer,
			// and [IdleAfter] is measured against ANY byte on the carrier.
		}
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
			stream.closeOnce.Do(func() { close(stream.closed) })
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
