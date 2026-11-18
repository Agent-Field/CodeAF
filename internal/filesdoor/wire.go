package filesdoor

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
)

// The wire inside the tunnel is one message after another:
//
//	u32 big-endian n | type byte | n-1 bytes of body
//
// THE LENGTH IS CHECKED BEFORE ANYTHING IS ALLOCATED, and nothing past the
// declared length is ever read. A message too big for its direction closes the
// connection: there is no benign reason to send one, and reading on would
// mean trusting a length the peer chose.
//
// Object ids travel as raw bytes (32 for the 64 hex characters), which keeps a
// request for a thousand ids under half of [MaxRequestBytes]. Everything that
// can be a number is a fixed-width big-endian number, so a body has exactly one
// valid length for its content and a short or long one is refused.
const (
	typeHello   byte = 'h' // json hello, both directions
	typeRefused byte = 'x' // json refusal: the door answered and said no
	typeError   byte = 'e' // one error code; the request itself was understood

	typeHas      byte = 1
	typeLocate   byte = 2
	typeGetMany  byte = 3
	typeGetFrame byte = 4

	// answerBit turns a request type into the type of its answer.
	answerBit byte = 0x80
)

// Error codes the door can answer with. They map one to one onto the store's
// own sentinels on the client, so a taker sees the error a local store gives.
const (
	codeBadRID     byte = 1
	codeTooMany    byte = 2
	codeDamaged    byte = 3
	codeInternal   byte = 4
	codeBadRequest byte = 5
)

// A get-many answer leads with one of these.
const (
	manyObjects  byte = 0 // the longest prefix that fits, at least one object
	manyAbsent   byte = 1 // the first object asked for is not held
	manyTooLarge byte = 2 // the first object is bigger than one response
)

// A get-frame answer leads with one of these.
const (
	frameFound  byte = 0
	frameAbsent byte = 1
)

const (
	// maxResponseMessage is the biggest message a client accepts from a door.
	maxResponseMessage = MaxResponseBytes + ResponseSlack
	// getFrameFixed is what precedes the chunk in a get-frame answer: status,
	// total length and offset.
	getFrameFixed = 1 + 8 + 8
	// coalesceBelow is the size under which a part of a message is copied into
	// the buffer holding its header, so a message of small parts is one write.
	coalesceBelow = 16 << 10
	// slice is the most one write to the tunnel carries, so the silence timer
	// moves with the bytes and not once per eight-mebibyte response.
	slice = 64 << 10
)

// errProtocol marks a peer that broke the wire rules, as opposed to a
// connection that merely ended. The first closes the tunnel and is not worth a
// second try; the second may be a door that changed process.
var errProtocol = errors.New("filesdoor: the other side broke the wire rules")

func protocolf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errProtocol, fmt.Sprintf(format, args...))
}

// readMessage reads one message of at most max bytes (type byte included). It
// returns the whole size on the wire, prefix included, for the traffic counts.
func readMessage(r io.Reader, max int) (typ byte, body []byte, wire int, err error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, 0, err
	}
	size := binary.BigEndian.Uint32(head[:])
	if size == 0 {
		return 0, nil, 0, protocolf("an empty message")
	}
	if uint64(size) > uint64(max) {
		return 0, nil, 0, protocolf("a message of %d bytes, the limit is %d", size, max)
	}
	buf := make([]byte, int(size))
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, 0, err
	}
	return buf[0], buf[1:], 4 + int(size), nil
}

// writeMessage writes one message made of parts. Small parts are gathered with
// the header so they leave as one write; a large part is written as it is,
// without a copy. It returns the size on the wire.
func writeMessage(w io.Writer, typ byte, parts ...[]byte) (int, error) {
	size := 1
	for _, p := range parts {
		size += len(p)
	}
	if size > maxResponseMessage {
		return 0, fmt.Errorf("filesdoor: a message of %d bytes is over the limit", size)
	}
	buf := binary.BigEndian.AppendUint32(make([]byte, 0, 5+coalesceBelow), uint32(size))
	buf = append(buf, typ)
	for _, p := range parts {
		if len(p) < coalesceBelow {
			buf = append(buf, p...)
			continue
		}
		if len(buf) > 0 {
			if err := writeAll(w, buf); err != nil {
				return 0, err
			}
			buf = buf[:0]
		}
		if err := writeAll(w, p); err != nil {
			return 0, err
		}
	}
	if len(buf) > 0 {
		if err := writeAll(w, buf); err != nil {
			return 0, err
		}
	}
	return 4 + size, nil
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// A watch closes something when nothing has happened for a while. It is armed
// when made and moves forward with every kick.
type watch struct {
	timer *time.Timer
	after time.Duration
}

func newWatch(after time.Duration, expire func()) *watch {
	return &watch{timer: time.AfterFunc(after, expire), after: after}
}
func (w *watch) kick() { w.timer.Reset(w.after) }
func (w *watch) stop() { w.timer.Stop() }

// kickReader and kickWriter move a watch forward with every byte that crosses,
// so a slow but live transfer is never mistaken for a silent one.
type kickReader struct {
	r io.Reader
	w *watch
}

func (k kickReader) Read(p []byte) (int, error) {
	n, err := k.r.Read(p)
	if n > 0 {
		k.w.kick()
	}
	return n, err
}

type kickWriter struct {
	w io.Writer
	k *watch
}

func (k kickWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		part := p
		if len(part) > slice {
			part = part[:slice]
		}
		n, err := k.w.Write(part)
		written += n
		if err != nil {
			return written, err
		}
		k.k.kick()
		p = p[n:]
	}
	return written, nil
}

// ridsBody packs ids as a count and then 32 raw bytes each. An id that is not
// 64 lowercase hex characters is the store's ErrBadRID, the same as a local
// store would say, and never reaches the wire.
func ridsBody(rids []string) ([]byte, error) {
	if len(rids) > 0xffff {
		return nil, fmt.Errorf("%w: %d ids", blobstore.ErrTooMany, len(rids))
	}
	body := make([]byte, 2, 2+32*len(rids))
	binary.BigEndian.PutUint16(body, uint16(len(rids)))
	for _, rid := range rids {
		raw, err := ridBytes(rid)
		if err != nil {
			return nil, err
		}
		body = append(body, raw...)
	}
	return body, nil
}

func ridBytes(rid string) ([]byte, error) {
	if !blobstore.ValidRID(rid) {
		return nil, fmt.Errorf("%w: %q", blobstore.ErrBadRID, rid)
	}
	raw, err := hex.DecodeString(rid)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("%w: %q", blobstore.ErrBadRID, rid)
	}
	return raw, nil
}

var (
	errMalformed   = errors.New("filesdoor: a request body is not the shape it declares")
	errTooManyRids = errors.New("filesdoor: more ids than one request may carry")
)

// parseRids is the inverse of ridsBody. A body whose length is not exactly
// what its count says is malformed, and a count over limit is errTooManyRids,
// which is answered rather than fatal.
func parseRids(body []byte, limit int) ([]string, error) {
	if len(body) < 2 {
		return nil, errMalformed
	}
	count := int(binary.BigEndian.Uint16(body))
	if len(body) != 2+32*count {
		return nil, errMalformed
	}
	if count > limit {
		return nil, errTooManyRids
	}
	rids := make([]string, count)
	for i := range rids {
		rid := hex.EncodeToString(body[2+32*i : 2+32*(i+1)])
		if !blobstore.ValidRID(rid) {
			return nil, errMalformed
		}
		rids[i] = rid
	}
	return rids, nil
}

// frameRequest is the body of a get-frame request: the frame id, where in the
// frame to start, and how much to send (0 for as much as one response takes).
func frameRequest(frame string, off uint64, n uint32) ([]byte, error) {
	raw, err := ridBytes(frame)
	if err != nil {
		return nil, err
	}
	body := make([]byte, 0, 44)
	body = append(body, raw...)
	body = binary.BigEndian.AppendUint64(body, off)
	return binary.BigEndian.AppendUint32(body, n), nil
}

func parseFrameRequest(body []byte) (frame string, off uint64, n uint32, err error) {
	if len(body) != 44 {
		return "", 0, 0, errMalformed
	}
	frame = hex.EncodeToString(body[:32])
	return frame, binary.BigEndian.Uint64(body[32:40]), binary.BigEndian.Uint32(body[40:44]), nil
}
