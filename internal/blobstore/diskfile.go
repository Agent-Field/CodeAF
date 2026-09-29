package blobstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		return pointer{}, errors.New("blobstore: malformed pointer")
	}
	off, offErr := strconv.ParseUint(parts[1], 10, 63)
	n, lenErr := strconv.ParseUint(parts[2], 10, 32)
	if offErr != nil || lenErr != nil {
		return pointer{}, errors.New("blobstore: malformed pointer")
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
// The data goes to a temporary file in the same directory, is synced, then
// renamed into place, so the final name never holds a partial file and an
// existing file is never overwritten.
func (d *Disk) writeOnce(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("blobstore: create directory: %w", err)
	}
	if err := d.stage(dir, path, data); err != nil {
		return err
	}
	return syncDir(dir)
}

// stage writes data to a temporary file beside path and renames it into place.
func (d *Disk) stage(dir, path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("blobstore: create temporary file: %w", err)
	}
	defer func() {
		if err != nil {
			os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err == nil {
		err = d.sync(tmp)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("blobstore: write %s: %w", filepath.Base(path), err)
	}
	return os.Rename(tmp.Name(), path)
}

// syncDir makes a rename in dir durable.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("blobstore: open directory: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !errors.Is(err, fs.ErrInvalid) {
		return fmt.Errorf("blobstore: sync directory: %w", err)
	}
	return nil
}
