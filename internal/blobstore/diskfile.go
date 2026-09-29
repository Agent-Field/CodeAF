package blobstore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// pointer says where one object lies inside a stored frame.
type pointer struct {
	Frame FrameID
	Off   uint64
	Len   uint32
}

// line is the pointer's on-disk form.
func (p pointer) line() []byte {
	return []byte(fmt.Sprintf("%s %d %d\n", p.Frame, p.Off, p.Len))
}

// parsePointer reads a pointer line strictly: a file that does not look like
// one is damage, and must not be followed as a path.
func parsePointer(line []byte) (pointer, error) {
	parts := strings.Fields(string(line))
	if len(parts) != 3 || !ValidRID(parts[0]) {
		return pointer{}, fmt.Errorf("%w: malformed pointer", ErrDamaged)
	}
	off, offErr := strconv.ParseUint(parts[1], 10, 63)
	n, lenErr := strconv.ParseUint(parts[2], 10, 32)
	if offErr != nil || lenErr != nil {
		return pointer{}, fmt.Errorf("%w: malformed pointer", ErrDamaged)
	}
	return pointer{Frame: parts[0], Off: off, Len: uint32(n)}, nil
}

// payloadStart is where the payload begins in a frame: object offsets in the
// header count from there, and a pointer counts from the start of the frame.
func payloadStart(frame []byte, h Header) uint64 {
	payload := uint64(0)
	for _, ref := range h.Objects {
		payload += uint64(ref.Len)
	}
	return uint64(len(frame)) - payload
}

// writeOnce durably creates path with data, and does nothing if path exists.
func (d *Disk) writeOnce(path string, data []byte) error {
	return d.writeAllOnce([]entry{{path, data}})
}

// asFull names a full disk or an exhausted quota ErrFull, which a caller
// answers by retrying later, and leaves every other error as it is.
func asFull(err error) error {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return fmt.Errorf("%w: %v", ErrFull, err)
	}
	return err
}
