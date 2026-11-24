package livedoor

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Wire opcodes for the live door multiplexer frame header.
const (
	OpOpen     uint8 = 1
	OpOpened   uint8 = 2
	OpRefused  uint8 = 3
	OpData     uint8 = 4
	OpCredit   uint8 = 5
	OpClose    uint8 = 6
	OpReset    uint8 = 7
	OpPing     uint8 = 8
	OpPong     uint8 = 9
	OpHelloAck uint8 = 10
)

// Closed reason vocabulary carried by OpRefused, OpClose, and OpReset frames.
const (
	ReasonNotRunning    = "not-running"
	ReasonNotAllowed    = "not-allowed"
	ReasonHolderGone    = "holder-gone"
	ReasonHolderOld     = "holder-old"
	ReasonRemoved       = "removed"
	ReasonPermitChanged = "permit-changed"
	ReasonLagging       = "lagging"
	ReasonFrameTooLarge = "frame-too-large"
	ReasonBudget        = "budget"
	ReasonIdle          = "idle"
	ReasonHostClosing   = "host-closing"
	ReasonTooMany       = "too-many"
)

const frameHeaderBytes = 9

// ErrFrameTooLarge reports that a frame payload length exceeded ChunkBytes.
var ErrFrameTooLarge = errors.New("livedoor: frame payload exceeds ChunkBytes")

// frame is the decoded representation of one wire frame:
// op u8 | chan u32 BE | n u32 BE | payload[n].
type frame struct {
	op      uint8
	channel uint32
	payload []byte
}

func isKnownOp(op uint8) bool {
	return op >= OpOpen && op <= OpHelloAck
}

// writeFrame encodes f onto w in a single write and returns the total wire
// byte count written.
func writeFrame(w io.Writer, f frame, maxChunk int) (int, error) {
	if maxChunk <= 0 {
		maxChunk = ChunkBytes
	}
	if len(f.payload) > maxChunk {
		return 0, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(f.payload), maxChunk)
	}
	buf := make([]byte, frameHeaderBytes+len(f.payload))
	buf[0] = f.op
	binary.BigEndian.PutUint32(buf[1:5], f.channel)
	binary.BigEndian.PutUint32(buf[5:9], uint32(len(f.payload)))
	copy(buf[frameHeaderBytes:], f.payload)
	n, err := w.Write(buf)
	if err != nil {
		return n, err
	}
	if n != len(buf) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

// readFrame reads the next known frame from r, skipping any unknown opcode after
// consuming its bounded payload so future additive ops do not break older peers.
// If any incoming frame declares n > maxChunk, readFrame returns ErrFrameTooLarge.
func readFrame(r io.Reader, maxChunk int, onFrameRead func(n int)) (frame, error) {
	if maxChunk <= 0 {
		maxChunk = ChunkBytes
	}
	var hdr [frameHeaderBytes]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return frame{}, err
		}
		op := hdr[0]
		ch := binary.BigEndian.Uint32(hdr[1:5])
		n := binary.BigEndian.Uint32(hdr[5:9])
		if uint64(n) > uint64(maxChunk) {
			return frame{}, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, maxChunk)
		}
		var payload []byte
		if n > 0 {
			payload = make([]byte, int(n))
			if _, err := io.ReadFull(r, payload); err != nil {
				return frame{}, err
			}
		}
		if onFrameRead != nil {
			onFrameRead(frameHeaderBytes + int(n))
		}
		if !isKnownOp(op) {
			continue
		}
		return frame{
			op:      op,
			channel: ch,
			payload: payload,
		}, nil
	}
}

func encodeUint32(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

func decodeUint32(b []byte) (uint32, bool) {
	if len(b) != 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(b), true
}
